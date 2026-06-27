# File Trash

## Description

I want to add a concept of "trash" so deleted files are recoverable for a short window instead of being gone immediately.

### FileTrash table

A new `FileTrash` table, following the same schema convention as `files`/`FileVersions` (autoincrement `Id` PK plus the listed columns, `CREATE TABLE IF NOT EXISTS` in the same Go constructor): `FileId`, `Path`, `Content`, `DateDeleted`, `Size`. No `VersionId` column — restoring a file always mints a fresh VersionId like any other write.

A new constant `TrashTTL`, set to `"30m"`, parsed with the same duration-parsing helper already used for the Term TTL constants in `retention.go` (units s/m/h/d).

### Deletion flow (UI, single file)

Deleting a file via the TabBar/FileTree context menu no longer shows a confirmation prompt. It immediately: deletes the file from disk, removes its `files` row, and inserts a `FileTrash` row (Content/Size copied from the file's current DB content). A toast then appears saying the file was deleted, with an "UNDO" action on its right edge.

The existing toast system (`store.js` / `App.vue`) is reused as-is — single global toast slot, a new toast still replaces whatever's showing. It just gains support for an optional action button. The delete-undo toast keeps the existing 4s auto-dismiss duration; no new duration constant.

Clicking UNDO calls the restore API (the same one described below) and, on success, behaves exactly like restoring via the Trash Modal: opens the restored file in the main editor and navigates CurrentFolder/breadcrumbs to its folder. If restore fails (e.g. it already expired or was emptied via "Empty Trash" from elsewhere), there's no special error handling — this is rare enough that the lack of redirect makes the failure self-evident.

### Deletion flow (folder)

Deleting a folder keeps its existing native `confirm()` prompt and recursive disk/DB deletion behavior unchanged. The only addition: every contained file gets a `FileTrash` row inserted (same as the single-file flow). No toast of any kind appears for folder deletion — not even an informational one without undo.

### Deletion flow (watcher / startup-detected)

When a file disappears from disk outside the app (fsnotify `Remove` in `watcher.go`) or the startup scan finds a tracked file missing from disk, the same logic applies: remove the `files` row, insert a `FileTrash` row. No toast is shown for this path either — stays silent like today's watcher behavior, recoverable only via the Trash Modal.

### Restore

A new restore API route takes a `fileId` and restores it to its original path. Restore logic:

- Reuses the **original FileId** (not a fresh one) — this reconnects the file's pre-existing FileVersion history immediately.
- If a file already exists at the original path, picks a new name using the **exact same `NextDuplicateName` logic** the Duplicate feature already uses — meaning it can land on `(2)`, not `(1)` (I was initially wrong that it should start at `(1)`; it should just match Duplicate's existing behavior exactly).
- If the original parent folder no longer exists on disk, recreates it (`MkdirAll`) before writing.
- The restored file is appended at `Max(OrderNum)+1` in its destination folder — the same rule as any newly created file.
- The write goes through the normal internal content-write path, so it gets a fresh VersionId and a RecentFileVersions entry like any other edit — no special-casing for the versioning pipeline.
- On success, the `FileTrash` row is deleted immediately.

This route is shared by both the toast's UNDO action and the Trash Modal's restore button.

### Trash Modal

A new modal, structurally similar to HistoryModal but different enough to be its own component. Opened via a new "Trash" row in the Sidebar, styled the same as the existing "Search" row and positioned immediately below it.

- Header just says "Trash".
- The sidebar is **always visible**, even on mobile — no collapse behavior at all (unlike HistoryModal).
- On open, fetches the trash list fresh (never cached across opens, no live refresh while open — same convention as HistoryModal). The list endpoint returns only `fileId`, `path`, `lineCount`, `length`, `dateDeleted` per row — no content. Sorted most-recently-deleted first.
- A small filter input sits at the sidebar's top-left. It does a case-insensitive substring match against `path`, operating purely on the already-fetched in-memory list — no refetch.
- Each sidebar item shows two lines: the original path (left-ellipsis truncated, so the filename stays visible) on line 1, and `DateDeleted` + `Size` on line 2. Size is formatted in B/KB/MB using binary units (1024 B = 1 KB, 1024 KB = 1 MB).
- Clicking a sidebar item calls a separate API route to fetch that trashed file's content on demand, then displays it read-only in the right pane, styled like HistoryModal's content viewer (own markup/CSS, not a shared component).
- The right pane has a footer with two text buttons: **Restore** (calls the same restore API as the toast's UNDO) and **Delete Forever** (permanently deletes that one FileTrash row immediately — no confirmation, since it's a single low-stakes item already in the trash).
- The sidebar also has a footer with an **Empty Trash** button that deletes every FileTrash row at once, gated by a native `confirm()` prompt.
- On a successful restore (from either button), the Trash Modal closes, the restored file opens in the main editor, and CurrentFolder/breadcrumbs navigate to match — the same behavior as the toast's UNDO.

### Background cleanup

No new background ticker. The existing FileVersioning process (the per-minute background job in `retention.go`) gets one more step: delete `FileTrash` rows whose `DateDeleted` is older than `TrashTTL`.

## Out of Scope

- No toast queue/stack — concurrent deletes still share the single global toast slot, so a second delete's toast replaces the first (the file is still safely in trash regardless, just not undoable via toast anymore).
- No toast (informational or otherwise) for folder deletion or watcher/startup-detected deletion.
- No live WebSocket updates to the Trash Modal's list while it's open.
- No special error UX for restore race conditions (already-expired/already-emptied) — default failure behavior (no redirect) is good enough.
- No confirmation prompt for the per-item "Delete Forever" action (only the bulk "Empty Trash" action gets one).
