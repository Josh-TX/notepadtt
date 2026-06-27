# Search Extended Sources

## Description

I want to wire up the "inert" search settings I added previously (`SearchHistory`, `SearchTrash`, `LinesPerResult`, `MaxResultsPerFile`) and extend Search so it can also search the FileVersions table (History) and the FileTrash table (Trash), not just live files. I also want a file that has multiple matching spots to show multiple result sections instead of just one snippet.

### Settings changes

- Drop `SearchRegex` entirely — remove the struct field, the DB column, the SettingsModal checkbox, and any mention in specs/settings.md. There are no production deployments yet, so just change the schema/struct directly rather than writing a migration.
- Add a new setting, `MaxFiles` (int, default 30). This replaces the previously-hardcoded "top 10" cap, which now applies across all sources combined, not just live files. Validate `>= 1`, no upper bound (same convention as `LinesPerResult`/`MaxResultsPerFile`). Settings-only — no live control in SearchModal.
- `SearchHistory` and `SearchTrash` (bools, default false) and `LinesPerResult` (default 4) / `MaxResultsPerFile` (default 2) go live as described below.

### SearchModal UI

- The "Initial Search Filters" naming was a hint: `SearchHistory` and `SearchTrash` get their own live checkboxes inside SearchModal itself, seeded from the Settings defaults but overridable per search session. Once toggled, the state persists in memory across modal close/reopen for the rest of the page session (same convention already used for `searchQuery`/`searchResults`), resetting to Settings defaults only on a full page reload.
- `LinesPerResult`, `MaxResultsPerFile`, and `MaxFiles` stay Settings-only — no SearchModal UI for these.
- Results render as a single unified list — Files results first, then Trash, then History (see Cross-source ranking and merging), not grouped/sectioned visually by source. Live-file results get no badge. History results get a small clock icon; Trash results get a small trash-can icon.
- No pagination/infinite-scroll — stays a flat list capped at `MaxFiles`. No "N more results" indicator when truncated; stays silent like today.

### Cross-source ranking and merging

bm25() in SQLite's FTS5 is reliable when used directly in a query's own `ORDER BY` clause, but not as a value read back out via `SELECT` — so scores are never extracted into application code, never compared across sources, and never used for any merge/collapse logic. There is no cross-source numeric ranking at all.

Instead, sources are queried in strict priority order — **Files, then Trash, then History** — each one only queried while result slots remain, and each fully exhausted (up to its share of `MaxFiles`) before the next is even queried:

- Files: `ORDER BY bm25(...) LIMIT MaxFiles`. One row per FileId is guaranteed by the table itself, so no dedup needed.
- Trash (only if `IncludeTrash` and slots remain): `ORDER BY bm25(...) LIMIT remaining`. A FileId can never have both a live `files` row and a `FileTrash` row (trashing always deletes the former before inserting the latter), so no exclusion against Files is needed, and FileTrash already guarantees ≤1 row per FileId.
- History (only if `IncludeHistory` and slots remain): queried one row at a time via `ORDER BY bm25(...) LIMIT 1`, looping until slots run out or a query returns nothing. Each iteration's query excludes every FileId claimed so far (by Files, by Trash, and by History's own prior iterations) via `WHERE FileId NOT IN (...)`, which is what stands in for "best per FileId" without ever reading a bm25 value out of SQLite — the single row returned is guaranteed to be both the best-ranked remaining match and a FileId not yet seen.

The final result list is simply Files results, then Trash results, then History results — no interleaving by relevance across sources. The old History×0.7/Trash×0.5 score-penalty constants are gone; they had nothing left to operate on.

### History (FileVersions) search

- Searches only the persisted `FileVersions` table via the existing (currently unused) `fileversions_fts` table. Does not scan the in-memory `RecentFileVersions` cache — edits in the last ~5s before they're persisted won't show up yet, which is an acceptable lag.
- A file can have many matching FileVersion rows across its history. These collapse to a single History result per FileId via the one-row-at-a-time exclusion loop described above (no score comparison) — this specifically guards against a file with many near-duplicate matching versions crowding out other distinct files' results.
- Matches are included even for orphaned FileIds — files whose live `files` row no longer exists. FileVersion rows persist independently of the live file's lifecycle (TTL-only expiry), so this is expected.
- Path shown in a History result: the file's current live path (looked up via FileId) if it still exists. Falls back to the FileVersion row's own stored historical path only when the file is fully orphaned.

### Trash (FileTrash) search

- Needs a new `filetrash_fts` FTS5 virtual table mirroring `fileversions_fts`'s design: trigram tokenizer, external content (`content='FileTrash'`, `content_rowid='Id'`), indexing Path+Content, kept in sync via `AFTER INSERT`/`BEFORE DELETE` triggers on `FileTrash`. FileTrash rows get deleted via several paths (restore, Delete Forever, Empty Trash, TTL sweep) — a single `BEFORE DELETE` trigger covers all of them since they're all just DB-level deletes.
- Path shown in a Trash result is always the FileTrash row's stored original path (trashed files have no live path by definition).

### Multi-section results (applies to all three sources)

- Result shape changes: one result object per file, containing an array of section objects (snippet text + startLineNumber each) instead of a single snippet.
- Selection: score every line in the matched content by case-insensitive substring match count (as today), take the top `MaxResultsPerFile` highest-scoring lines as candidate match points.
- Context window per candidate, sized by `LinesPerResult`:
  - If `LinesPerResult >= 3`: 1 line before, the match line, then `LinesPerResult - 2` lines after.
  - If `LinesPerResult <= 2`: 0 lines before, the match line, then `LinesPerResult - 1` lines after.
  - (This generalizes today's hardcoded "1 before + match + 2 after" = 4 total, for the default `LinesPerResult=4`.)
- If two candidate windows overlap or touch, merge them into one continuous section, even if it ends up longer than `LinesPerResult`. No cap on merged section length.
- No backfill: if merging reduces the final section count below `MaxResultsPerFile`, leave it as-is rather than pulling in more candidates.
- Final sections for a file are ordered top-to-bottom by line number for display (not by relevance).
- Each section is independently clickable/navigable to its own line, not just the file/path header.

### Click-through behavior

- Files-source result: unchanged — navigate folder if needed, open tab, scroll to line (or just scroll/highlight if it's already the active tab).
- History-source result: opens HistoryModal for that FileId, auto-selecting the matching FileVersion entry, scrolled to and highlighting the matched section's line(s). If the file currently exists live, CurrentFolder navigates to it first, same as a Files click. If the file is orphaned, CurrentFolder navigation is skipped (nothing live to go to) but HistoryModal still opens.
- Trash-source result: opens TrashModal, auto-selecting that trashed file, scrolled to and highlighting the matched section's line(s).

### HistoryModal "Trashed Content" fallback

This is a general HistoryModal gap, not search-specific, but search is what surfaces it: when HistoryModal is opened for a FileId whose live file doesn't currently exist, the pinned top-of-sidebar entry that normally reads "Current Snapshot":

- Instead reads "Trashed Content" and shows the content from that FileId's current FileTrash row, if one exists.
- Is omitted entirely if no FileTrash row exists either (fully expired — nothing current to show; only past FileVersion rows are listed).
- Stays view-only, same as today's HistoryModal — no restore button added here. Restore still only happens via TrashModal or the original delete-toast's UNDO.

### Migrations

No production deployments exist yet, so schema changes (dropping the `SearchRegex` column, adding `filetrash_fts` + triggers) are made directly in the schema-setup code, not via an ALTER-based migration.

## Out of Scope

- Regex search support — dropped entirely, not left inert for later.
- Comparing or merging bm25 scores across sources at all — reading bm25() out of SQLite via SELECT proved unreliable, so ranking relies solely on each source's own `ORDER BY bm25(...)` and a strict Files-then-Trash-then-History priority order instead.
- Pagination or "load more" for search results.
- A truncation indicator when results are capped at `MaxFiles`.
- A restore action inside HistoryModal's new "Trashed Content" entry.
- Searching the in-memory `RecentFileVersions` cache for History search.
