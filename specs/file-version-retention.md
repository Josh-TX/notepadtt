# File Version Retention

## Description

I want to add tiered version history for files: a background process periodically snapshots recent content into a `FileVersions` table, retaining snapshots longer the older/sparser they are (short/medium/long/very-long tiers), and expiring them once they age past each tier's TTL. This requires first splitting the `files` table out of its current FTS5-virtual-table form into a normal table plus a separate FTS5 index table, since `FileVersions` needs a real table to live alongside (foreign-key-style references, real indexes) and the new debounced FTS sync approach needs a real content table to sync from.

This is schema + background-process work only — no API or UI for browsing/searching/restoring old versions yet (see Out of Scope).

### `files` table becomes a normal table

Today `files` is created as `CREATE VIRTUAL TABLE files USING fts5(...)` (`backend/db.go`) — it's both the content store and the search index in one. Split this into:

- `files`: a normal table with `Id INTEGER PRIMARY KEY AUTOINCREMENT`, `FileId TEXT UNIQUE`, `Path TEXT UNIQUE`, `Content`, `LastOpened`, `ContentUpdated`, `OrderNum`, `VersionId`. `FileId` and `Path` get `UNIQUE` constraints now that a real table can enforce them (FTS5 virtual tables can't) — both were already application-level invariants, this just adds a DB-level backstop.
- `files_fts`: a new FTS5 virtual table, `content=files`, `content_rowid=Id`, `tokenize='trigram'`. Only declares `Path` and `Content` columns (the other columns don't need FTS indexing — UNINDEXED placeholders aren't needed anymore since the real values live in `files` and can be joined back to via rowid).

No migration path is needed — only a dev database exists today and it'll be deleted; the new schema is simply created fresh.

`backend/search.go`'s queries need to be rewritten to query `files_fts` (for the bm25-ranked match) joined back to `files` via rowid (for `FileId`, `OrderNum`, etc. needed in results). The ranking/weighting behavior (path 3x over content, top 10) is unchanged.

### Keeping `files_fts` in sync

`files.Content` is updated on every single keystroke (today's edit flow writes to the DB on every CodeMirror change event). A synchronous trigger that re-tokenizes trigram content on every write would put that cost on the hot path, so `files_fts` sync is debounced application code, not SQL triggers:

- Every call site that mutates `files` (insert, content update, path rename, delete — across `db.go`, `watcher.go`, `edit.go`) calls a single shared `MarkDirty(fileId)` that adds the FileId to an in-memory dirty set. This is uniform across create/edit/rename/delete — one code path, no special-casing per mutation type.
- Flushing uses a hybrid "debounce with max wait": a 2-second timer resets every time a new FileId is marked dirty; a separate 10-second timer starts on the first mark of a batch and does not reset on subsequent marks. Whichever fires first triggers the flush, then both reset for the next batch. This guarantees `files_fts` is never more than 10 seconds stale while still collapsing rapid bursts (e.g. continuous typing) into one flush, 2 seconds after things go quiet.
- The dirty set and its timers are global (one shared set + one timer pair, not one per FileId) — matches the existing debounce idiom already used in `ws.go`'s `BroadcastFS`/`flushTree` (`time.AfterFunc`), and avoids juggling a timer pair per actively-edited file.
- On flush, for each currently-dirty FileId: look up its current row in `files`. Maintain a small in-memory cache, `FileId -> last-synced (Path, Content)`, reflecting what's currently indexed in `files_fts`. If a cache entry exists, issue the FTS5 `'delete'` command using those *old, cached* values (`INSERT INTO files_fts(files_fts, rowid, Path, Content) VALUES('delete', $rowid, $oldPath, $oldContent)`) — by flush time `files.Content` already holds the newest keystroke's value, not what's actually still reflected in the index, so the old values must come from this cache, not from re-reading `files`. If the file still exists in `files`, insert the fresh values and update the cache; if it no longer exists (deleted), skip the insert and drop the cache entry.
- Why the cache is necessary at all: `files_fts` is an external-content table — it stores only the inverted index (token postings per rowid), never a copy of the text itself. Computing which tokens to remove for a given rowid requires the actual old text (FTS5 tokenizes whatever values you pass to the `'delete'` command); since `files_fts` keeps no copy to fall back on, and `files` itself has already moved on to newer content by flush time, the old text has to come from somewhere durable across flushes — hence the cache. Passing the wrong (already-new) values to `'delete'` would compute the wrong tokens, silently leaving orphaned tokens in the index or failing to remove tokens still present in the new content.
- This in-memory cache is empty after every server restart, but `files_fts` itself persists across restarts (it's part of the SQLite file) and should already be in sync as of the last clean shutdown. So right after `startupScan()` finishes rebuilding `files` from disk, seed the cache with every row's current `(Path, Content)`. If the server crashed mid-debounce-window, up to 10s of pending changes could be missed by this seeding — accepted as a self-healing edge case, since the next edit to that file triggers a fresh, correct resync anyway.

### `FileVersions` table

A new table holding historical content snapshots:

- `Id INTEGER PRIMARY KEY AUTOINCREMENT`
- `FileId`
- `Path` — the file's path at the time this version was current
- `Content` — the full content of this version
- `VersionId` — the VersionId string this version had
- `Date` — unix millis (consistent with `LastOpened`/`ContentUpdated`); the moment this version became non-current
- `Term` — int 1-4, cumulative retention tier (1=short only, 2=short+med, 3=+long, 4=+verylong)

Indexes: `(FileId, Term, Date)` and `(Term, Date)`, supporting the per-FileId date lookups and the TTL sweep described below.

Every `FileVersions` row is sourced **entirely** from a `RecentFileVersions` entry — `FileId`/`Path`/`Content`/`VersionId` are copied verbatim and `Date` is that entry's `addedAt`. `Files.Content` is never re-read for this purpose. No `RecentFileVersions` entry is excluded as a candidate — including the "naive" non-DB-committed entry that's cached purely to let a racing client's chained edit resolve against it (see `RecentFileVersions` in the glossary); persisting it occasionally as a `FileVersion` is an accepted, harmless edge case rather than something worth special-casing around.

`FileVersions` rows are immutable: inserted once by the FileVersioning process, never updated, eventually hard-deleted by the TTL sweep. Deleting the underlying file does **not** cascade-delete its `FileVersions` rows — they age out naturally via their own TTL. (A future "restore deleted files" feature will rely on this; out of scope here.)

### `fileversions_fts`

A second new FTS5 virtual table, `content=FileVersions`, `content_rowid=Id`, `tokenize='trigram'`, declaring only `Path` and `Content` columns (mirrors `files_fts`'s column choice).

Unlike `files_fts`, this is synced via plain SQL triggers — safe here specifically because `FileVersions` rows are immutable (inserted once by a once-a-minute process, never updated, only ever deleted by the TTL sweep), so there's no per-keystroke volume concern and no stale-old-value risk (the row's content never changes between insert and delete, so a `BEFORE DELETE` trigger can read `old.*` directly):

```sql
CREATE TRIGGER fileversions_ai AFTER INSERT ON FileVersions BEGIN
  INSERT INTO fileversions_fts(rowid, Path, Content) VALUES (new.Id, new.Path, new.Content);
END;

CREATE TRIGGER fileversions_bd BEFORE DELETE ON FileVersions BEGIN
  INSERT INTO fileversions_fts(fileversions_fts, rowid, Path, Content) VALUES ('delete', old.Id, old.Path, old.Content);
END;
```

No `UPDATE` trigger is needed. This table is not wired into any user-facing search yet — it exists ahead of a future full-text-search-over-history feature (out of scope here).

### Retention constants

New hardcoded Go constants (no user-configurable settings yet — that's planned for later):

```
ShortTermTTL      = 10m   ShortTermMinDelay    = 40s
MedTermTTL        = 120m  MedTermMinDelay      = 8m
LongTermTTL       = 7d    LongTermMinDelay     = 18h
VeryLongTermTTL   = 60d   VeryLongTermMinDelay = 5d
```

These need a small duration-parsing helper: case-insensitive units `s`/`m`/`h`/`d` only, with an optional space between the number and the unit (e.g. `"8m"`, `"8 M"` both parse to 8 minutes).

### `RecentFileVersions` changes

- Each entry gains a `path` field (alongside the existing `fileId`, `versionId`, `content`, `addedAt`), since `FileVersions` rows need a `Path` and that value isn't otherwise available at persist time.
- The existing 5-second-old purge is removed from its own standalone ticker (`RecentVersionStore.cleanupLoop`, currently ticking every minute) and instead happens as the **final step** of the new FileVersioning process below — same 5-second threshold, same slice-compaction logic, just relocated so it runs after that process has had a chance to look at (and persist from) whatever's currently in the list.

### The FileVersioning process

Replaces the existing once-a-minute `RecentVersionStore` cleanup loop with a bigger process, still running once a minute. Each run, in order:

1. **Delete expired `FileVersions` rows.** TTL is selected by each row's own `Term`: `Term=1` rows expire past `ShortTermTTL`, `Term=2` past `MedTermTTL`, `Term=3` past `LongTermTTL`, `Term=4` past `VeryLongTermTTL`. Roughly:
   ```sql
   DELETE FROM FileVersions WHERE
     (Term=1 AND Date < now-ShortTermTTL) OR
     (Term=2 AND Date < now-MedTermTTL) OR
     (Term=3 AND Date < now-LongTermTTL) OR
     (Term=4 AND Date < now-VeryLongTermTTL)
   ```
2. **Collect distinct FileIds** currently present in `RecentFileVersions`.
3. **One grouped query** across those FileIds to get, per FileId, the most recent `Date` for each cumulative term threshold — `Term` values are cumulative (a `Term=3` row counts toward `Term>=1`, `>=2`, and `>=3`), so this is `MAX(Date) WHERE Term>=N` for `N` in 1..4, computed for all FileIds in a single query:
   ```sql
   SELECT FileId,
     MAX(CASE WHEN Term>=1 THEN Date END) t1,
     MAX(CASE WHEN Term>=2 THEN Date END) t2,
     MAX(CASE WHEN Term>=3 THEN Date END) t3,
     MAX(CASE WHEN Term>=4 THEN Date END) t4
   FROM FileVersions WHERE FileId IN (...) GROUP BY FileId
   ```
4. **For each FileId**, walk its `RecentFileVersions` entries in chronological order (oldest `addedAt` first), maintaining a running `lastDate[1..4]` per FileId seeded from step 3:
   - If `entry.addedAt - lastDate[1] < ShortTermMinDelay`, skip this entry — not persisted.
   - Otherwise, determine the row's final `Term`: starting at `N=2`, keep bumping `Term` to `N` while `entry.addedAt - lastDate[N] >= MinDelay[N]`, stopping at the first `N` that fails. (Since MinDelay/TTL values are strictly increasing across tiers, satisfying a higher tier's delay always implies satisfying every lower tier's, so this can simply walk `N=2,3,4` in order.)
   - Insert a `FileVersions` row: `FileId`, `Path`, `Content`, `VersionId`, `Date=entry.addedAt`, `Term=<final Term>` — all five values taken directly from this `RecentFileVersions` entry.
   - Update `lastDate[N] = entry.addedAt` for every `N` ≤ the final Term, so later entries processed within this same run see the update.
   - This means more than one `FileVersions` row can be persisted for the same FileId in a single run (e.g. two entries far enough apart in time both clear `ShortTermMinDelay`). This matters for future-proofing: `RecentFileVersions`'s retention window or `ShortTermMinDelay` may both change later, and this design keeps working correctly either way.
5. **Purge `RecentFileVersions` entries older than 5 seconds** (logic moved from the old standalone cleanup loop, described above).

This design is self-correcting against double-persisting: because `Date` is written verbatim from the source entry's `addedAt`, and the per-term "last date" lookup (step 3) is freshly re-queried at the start of every run, an entry that happens to survive un-purged into a later run (because it was too fresh, under 5s old, to be purged at the end of the run it was first evaluated in) gets re-evaluated against a `lastDate[1]` that's now equal to its own `addedAt` — a zero gap, which is always less than `ShortTermMinDelay` — so it's correctly skipped rather than persisted twice.

### Startup

`startupScan()` continues to rebuild `files` from disk as it does today (recovering `Path`/`Content` from the filesystem; `OrderNum`/`LastOpened`/`VersionId` aren't recoverable from disk and are handled as today). Additionally, once it finishes, seed the `files_fts` in-memory "last-synced" cache (described above) from the resulting `files` rows.

## Out of Scope

- Any API or UI for browsing, searching, or restoring old `FileVersions` rows — this is schema + background-process plumbing only.
- Wiring `fileversions_fts` into any actual search feature — it's created and kept in sync, but unused, ahead of a future full-text-search-over-history feature.
- Cascading deletion of `FileVersions` rows when their underlying file is deleted — they age out via TTL only. A future "restore deleted files" feature will rely on this; not built here.
- User-configurable retention settings — the 8 TTL/MinDelay values are hardcoded Go constants for now.
- Migrating existing production data — only a dev database exists today and it will be deleted/recreated against the new schema.
