# notepadtt Initial Implementation
Date: 2026-06-12

Full initial implementation of notepadtt: a Go web server with SQLite backend serving a Vue 3 + CodeMirror 5 SPA for editing text files. Covers layout (navbar, tabbar, editor, footer, sidebar), FileId-centric file identity, WebSocket live sync, REST API for mutations, filesystem watching, VS Code-style dark UI, and new-file naming conventions.

# Tab Ordering
Date: 2026-06-13

Introduces an OrderNum column to the SQLite files table to control per-folder file display order in the TabBar and sidebar FileTree. Defines assignment rules for new, duplicated, externally-discovered, and deleted files. Adds a PUT /api/files/:id/order endpoint for reordering. Implements HTML5 drag-and-drop tab reordering on desktop with a drop indicator line and edge auto-scroll.

# Mobile Layout and Sidebar Polish
Date: 2026-06-13

Fixes a mobile layout bug where the navbar could scroll off-screen by removing the app-shell wrapper and making the sidebar always position:fixed. Adds a 150ms sidebar slide animation with a synchronized margin-left transition on desktop, and a tap-to-dismiss semi-transparent overlay on mobile. Replaces the sidebar close button with a double-chevron collapse button in the top-left so there is always a toggle in the viewport corner. Adds a visual root folder row to the FileTree, switches expand/collapse icons to SVG chevrons, removes file icons, and standardizes font sizes across components. Also fixes a bug where clicking a file in the current folder from the sidebar had no effect.

## [patch] 2026-06-13
Fixed a null-ref crash in context menu actions caused by the close event firing before the action callback. Added a file context menu to the sidebar FileTree (rename, delete, duplicate, download). Disabled HTML5 drag-to-reorder on touch devices to restore long-press-to-contextmenu behavior. Clamped context menu position to the viewport to prevent overflow off the right or bottom edge. Tabs and FileTree rows now maintain their hover style while their context menu is open.

## [bug] 2026-06-13
Fixed new-file naming returning wrong N because stale DB entries for externally-deleted files were never pruned. startupScan now removes DB records for files not found on disk, and the watcher's Remove handler now deletes from DB immediately. New files are also initialized with 4 newlines (5 lines) instead of empty content.

## [patch] 2026-06-13
Replaced the static "NTT" brand in the Navbar with a "root" breadcrumb that is always present. Root is a plain span when already at root, and a clickable router-link otherwise. Removed the mobile-only restriction on breadcrumb links — all non-rightmost crumbs are now clickable on all devices. Bumped non-link crumb and separator contrast slightly.

## [patch] 2026-06-13
Made the Navbar breadcrumb area horizontally scrollable by wrapping it in a navbar-wrapper and moving the sidebar-toggle and new-file buttons to be absolutely positioned. Buttons have a solid background so breadcrumbs scroll cleanly underneath. Set global color-scheme: dark so browser-native UI controls (scrollbars, inputs) render in dark theme.

# Concurrent Edit Versioning
Date: 2026-06-14

Introduces VersionId (5-char alphanumeric) to the files table and all content payloads to detect and resolve concurrent edits. Replaces UUID-based FileId with a 12-char alphanumeric uniqueId. Clients optimistically assign their own newVersionId on each write; the backend compares against the stored VersionId and either accepts the write, attempts a 3-way merge via go-diff when the client's prior version is found in the in-memory RecentFileVersions cache, or returns a 409 with current content so the client can recover. RecentFileVersions entries expire after 5 seconds.

# FTS5 Search
Date: 2026-06-16

Adds workspace-wide file search powered by SQLite FTS5 with a trigram tokenizer. Replaces the regular `files` table with a single FTS5 virtual table (Path and Content indexed, all other columns UNINDEXED). A new GET /api/search?q= endpoint splits the query on whitespace, OR-joins terms, ranks by bm25 with path weighted 3×, and returns up to 10 results each with a 4-line SearchSnippet around the best-matching line. The Sidebar gains a clickable Search row above the FileTree that opens a SearchModal — full-screen on mobile, large centered on desktop — with a debounced input, subtle loading indicator, yellow/orange term highlighting, and persistent query/results state across open/close cycles. Clicking a result navigates to the file's folder, opens the tab, and scrolls the editor to the matching line.

## [bug] 2026-06-17
Fixed a race where the fsnotify watcher could double-track a file the create/duplicate API handler had just written, by adding a short-lived RecentlyCreatedStore the watcher checks before inserting a DB row. File creation is now atomic: the backend generates the "new N" name and returns the full file (fileId, path, content, versionId) in one response, removing the old two-step flow where the frontend fetched the next N via /api/next-new-n then polled until the file appeared in the tree.

# Incremental Edit Sync
Date: 2026-06-18

Replaces the HTTP PATCH /api/files/:id full-content write with a WebSocket-based protocol that sends only the CodeMirror change event (from/to/text/removed) per keystroke, no batching. The backend's previously no-op WS readPump now handles an `edit` message type: on a version match it applies the change directly; on a mismatch it looks up the client's old snapshot in RecentFileVersions and attempts relocation via a diffmatchpatch line-mode diff against the latest content, reapplying the change at its shifted position if the touched lines are otherwise untouched. Resolved merges commit under a freshly minted VersionId and broadcast to all clients including the sender, while also stashing the naive (non-merged) result under the client's own newVersionId so chained follow-up edits can still resolve their base. Unresolvable conflicts reject via a new targeted `editConflict` message (latest content + version) sent only to the offending sender, who shows the existing override toast.

# Minimal Edit Apply
Date: 2026-06-19

Fixes a bug where a remote content update arriving via WebSocket (`content` or `editConflict`) could silently revert a user's just-typed keystroke, because `Editor.vue` applied it with `cm.setValue`, which resets CodeMirror's input state and destroys any in-flight (not-yet-polled) keystroke. Adds a pure `computeMinimalEdit(oldStr, newStr)` function in `frontend/src/cmEdit.js` that trims the common prefix/suffix between old and new content and returns a single `{from, to, text}` edit describing the smallest possible change. Editor.vue now applies remote updates via `cm.replaceRange` with this computed edit instead of `setValue`, letting CodeMirror's live cursor tracking handle position adjustment automatically, with a try/catch fallback to the original `setValue` behavior for safety. No backend changes; the server still broadcasts full file content. The tab-switch `setValue` path is untouched.

# File Version Retention
Date: 2026-06-20

Splits the `files` table out of its current single-FTS5-virtual-table form into a normal table plus a separate, debounce-synced `files_fts` index table, then adds a new `FileVersions` table (with its own trigger-synced `fileversions_fts` index) for tiered version history. A background "FileVersioning process" replaces the old once-a-minute RecentFileVersions cleanup: it expires old FileVersions rows by TTL, persists eligible RecentFileVersions entries into FileVersions under a cumulative short/med/long/verylong Term based on hardcoded TTL/MinDelay constants, then purges RecentFileVersions as its final step. Schema and background process only — no browsing/search/restore API yet.

# History Modal
Date: 2026-06-20

Adds a HistoryModal for browsing a file's past content, opened via a new "History" option in the TabBar/FileTree context menus and a new Footer button. A new `GET /api/files/{id}/versions` endpoint returns persisted FileVersions rows merged with on-the-fly previews of RecentFileVersions entries that would become eligible on the FileVersioning process's next run (computed via logic extracted into a shared function, never persisted by the preview path). The modal pins a "Current Snapshot" entry at top (frozen editor state, or fetched via a tracking-free `GET /api/files/{id}` call for inactive files) above a flat, newest-first list of past versions with date/length/line stats, alongside a read-only, non-wrapping content viewer. Sidebar is fixed-width on desktop and collapsible with a 150ms animation on mobile, capped at 60% of modal width either way. View-only — no restore action.

# File Trash
Date: 2026-06-20

Adds a soft-delete "trash" concept: deleting a file (via UI, on-disk removal, or startup-missing detection) now inserts a FileTrash row instead of being permanently gone, governed by a 30-minute TrashTTL swept by the existing FileVersioning process. UI deletion drops its confirmation prompt in favor of an undo toast; a new restore API (reusing the original FileId and the existing Duplicate-rename logic) is shared by the toast's UNDO and a new always-sidebar-visible Trash Modal, which lists trashed files, previews content on demand, and supports restore, permanent per-item delete, and bulk empty-trash.

# Settings
Date: 2026-06-21

Adds a single-row Settings table loaded by the frontend on every refresh, with a GET/PUT API and a new Settings button in a new sidebar footer section opening a SettingsModal. Makes TrashTTL and the 8 File-Version-Retention TTL/MinDelay values (previously hardcoded constants) live-configurable via an in-memory cache the FileVersioning process reads each run, with format and strictly-increasing-tier validation. Also adds Tab Mode, Tab Close Icon, and Search-filter-default settings as inert placeholders for a later feature, and a Word Wrap mode setting that actually replaces today's flat session-only wrap toggle with per-device (localStorage) or per-file (in-memory, session-only) persistence.

# Search Extended Sources
Date: 2026-06-21

Wires up the previously-inert SearchHistory/SearchTrash/LinesPerResult/MaxResultsPerFile settings (dropping SearchRegex entirely) and extends Search to also query the FileVersions (History) and FileTrash (Trash) tables alongside live files, merging all three into one bm25-ranked list with hardcoded penalty multipliers for History/Trash and per-FileId suppression when a live-file match exists. Adds a new MaxFiles setting (default 30) replacing the old hardcoded top-10 cap. Generalizes snippet extraction to return multiple merged, top-to-bottom-ordered match sections per file (bounded by MaxResultsPerFile, sized by LinesPerResult) instead of a single snippet. History/Trash results open HistoryModal/TrashModal scrolled to the match; HistoryModal also gains a "Trashed Content" fallback for files whose live row no longer exists.

## [patch] 2026-06-21
Removed the SearchHistory/SearchTrash settings entirely — SearchModal's History/Trash checkboxes now always start unchecked instead of being seeded from a configurable default. Replaced the 4-mode WordWrapMode setting (device/file × on/off, with localStorage and per-file tracking) with a single global `WordWrap` boolean, default true, updated via its own `PUT /api/settings/wordwrap` endpoint triggered by the Footer's "Wrap" button rather than the Settings modal's full-form save.

# Sidebar Resize
Date: 2026-06-23

Adds two new Settings fields: `EditorFontSize` (10-24px, default 14, editable via the Settings modal's full-form save like any other field) and `SidebarWidth` (100-600px, default 400, updated only via a new dedicated `PUT /api/settings/sidebarwidth` endpoint mirroring the WordWrap pattern, never exposed in the modal). Makes the Sidebar draggable-resizable on its right edge via Pointer Events, gated by a new `isPushLayout` reactive flag (matchMedia 768px) combined with the existing touch-device check, so the handle only exists in the DOM on desktop with a cursor device. Width and main-layout margin update live during the drag with the open/close CSS transition suspended, clamped client-side to the same bounds, persisting once on release with revert-on-failure.

# Sidebar Resize
Date: 2026-06-23

Implements the Sidebar Resize spec end-to-end, and adds a third persisted setting beyond the original plan: `DesktopSidebarOpen`, which remembers whether the desktop push-layout sidebar is open or closed across reloads via its own `PUT /api/settings/desktopsidebaropen` endpoint, persisted from both the Navbar toggle and the Sidebar collapse button (desktop-only, with revert-on-failure). Settings loading moves from `App.vue`'s `onMounted` to `main.js`, awaited before the app mounts, so the restored open/closed state doesn't animate the sidebar's slide transition on page load. The resize handle ships as a 6px hit-area with a neutral hover background, with the accent-color highlight shown as a border on the Sidebar's edge while dragging rather than on the handle itself.

# FileTree Drag Move
Date: 2026-06-24

Adds desktop-only drag-and-drop within the Sidebar's FileTree to move files and folders between folders, with hover-driven drop-target resolution (folder row, file row's parent, or root), an adaptive dwell-to-auto-expand timer for collapsed folders, vertical edge auto-scroll, and theme-consistent inset-shadow/background hover feedback mirroring TabBar's existing reorder drag conventions. Adds two new API routes mirroring the existing rename routes but taking a full destination path instead of a new name — move-file auto-resolves destination name conflicts via the existing `NextDuplicateName` logic and reopens the file in its new folder on success, while move-folder rejects on name conflicts or cyclical moves (validated both client- and server-side) and reuses `UpdateFolderPath`'s prefix-replace cascade.

# Move File Modal
Date: 2026-06-26

Adds a "Move" context menu item (after "Rename") in both the TabBar and FileTree file/folder menus, opening a new MoveFileModal. The modal title is "Move <original-path>". A text input pre-filled with the item's full path is paired with a Move button; pressing Enter triggers Move. The modal body has breadcrumbs (root="/") above a folder browser showing all immediate subfolders of the input's current directory; clicking a subfolder updates only the directory portion of the input, preserving the filename. Move is disabled (with inline conflict error) when a conflict is detected in real-time against the in-memory FileTree, or disabled silently when the path is unchanged. The backend's move-file endpoint gains MkdirAll support so typing a novel folder path implicitly creates it, and drops the auto-rename-on-conflict behavior in favor of returning 409; move-folder similarly gains MkdirAll.

# Syntax Highlighting
Date: 2026-06-29

Adds CodeMirror 5 syntax highlighting for a curated set of languages (JavaScript/TypeScript/JSON, Python, Markdown, YAML, HTML, CSS, Shell, SQL), detected automatically from the filename via `findModeByFileName`. Introduces a new `MarkdownMode` int setting (0=all new files, 1=all files without an extension, 2=only .md files) controlling when markdown highlighting applies to files with no recognized extension. The active language is displayed as a text label in the Footer on wide (≥768px) viewports, showing "text" for plain files.

# Sqlite Regex Search
Date: 2026-07-26

Removes SQLite FTS5 from search entirely (dropping all FTS5 virtual tables, shadow tables, and sync/trigger code) in favor of querying the existing `files`/`FileVersions`/`FileTrash` tables directly, since FTS5 was overkill for small workspaces. Adds a persistent "regex" checkbox to SearchModal: non-regex mode keeps today's whitespace-split OR'd terms but switches matching to SQL `LIKE` narrowing (benchmarked ~2.6-3.2x faster than a Go-side loop), while regex mode runs a per-line Go `regexp` match with no cross-line matching. Replaces bm25 ranking with a manually computed score (occurrence counts weighted by path/content and by source, plus a distinct-term multiplier in non-regex mode) that merges and sorts all three sources into one ranked list.
