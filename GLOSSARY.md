# Glossary

**FileId** — 12-char alphanumeric id assigned (in memory only) when a file gets a tab. Stable across renames/moves while the server runs; forgotten when the tab closes. All edit/WS/tab logic uses it. The file tree itself is path-based.

**VersionId** — 5-char alphanumeric id of a file's current in-memory content. Minted when content is loaded, on every client edit (client-chosen `newVersionId`), conflict merge, and external disk change. Included in content reads, `content` broadcasts and `editConflict` messages; the server compares an edit's `currentVersionId` to detect concurrent edits.

**RecentFileVersions** — in-memory `(fileId, versionId) -> content` snapshots, last 16 per file (≤8MB), dropped when the tab closes. Used to relocate an edit made against an older version onto the latest content (line diff). The sender's naive (non-merged) result is stored under its own `newVersionId` so its next chained edit still resolves.

**Open file registry** — server-side maps `fileId <-> path` plus loaded content. Entries exist only for tabs (and for trashed tabs awaiting UNDO). Content is loaded when a client fetches the file; large files are unloaded once no client is subscribed.

**Tab list** — ordered list of open files, held in server memory and shared live across all clients (open/close/reorder broadcast as `{type:"tabs"}`). Empty at server start; lost on restart. Reordering is manual.

**Active tab** — per client. The server only remembers the last-activated tab to hand to newly connecting clients.

**Data root** — directory served (`-d`). Everything is confined to it. `.git`, `node_modules` and `.ntt-trash` are treated as nonexistent.

**Disk sync** — edits apply in memory and are written behind to disk (200ms idle / 1s max; flushed before rename/delete/duplicate/download/tab close/shutdown). No save button. While a file is dirty or mid-write, watcher events for it are ignored. An fs watcher (whole root, recursive, follows dir symlinks) feeds external changes to open files through the same versioned stream as client edits (own writes are ignored by content comparison) and pushes tree updates. A file deleted/renamed externally closes its tab.

**New file** — `new N` (no extension), lowest unused N on disk. The TabBar "+" creates in the data root; a tree folder's context menu creates in that folder. New files are opened as a tab.

**Large file** — over 1000 KB (`maxDisplayBytes`): the tab shows a message and a "Display anyway" button; the choice is per client. **Binary file** — NUL byte in the first 8000 bytes: message + Download button only, never editable.

**Trash (`.ntt-trash`)** — deleting a file or folder moves it into `<root>/.ntt-trash/<id>/` for 5s then removes it permanently. Tabs under it close in all clients. Only the deleting client gets the toast with UNDO, which restores the entry and its tabs at their old positions (fails with "path exists" if the path was reoccupied). The folder is wiped at startup.

**Search** — Search Modal (Ctrl+Shift+F or the magnifier in the TabBar). The server shells out to `rg` (or `grep`) over the data root plus a filename match; filename matches rank first; top 10 results with 4-line snippets. Plain Ctrl+F is the browser's native find (works because the Editor uses `viewportMargin: Infinity`).

**TabBar** — the only top bar: sidebar toggle, tabs, "+" new-file button, search button. Tab shows the filename; hover title is the absolute path; the close button is always visible and closing never deletes the file. Right-click: Rename, Move, Delete, Duplicate, Download. Closing the active tab activates its right neighbor, else left.

**Sidebar** — FileTree only (no search/trash/settings rows). Overlays on mobile, pushes layout on desktop, resizable. Right-click folders (New File, New Folder, Rename, Move, Delete) and files (Rename, Move, Delete, Duplicate, Download); right-click on blank space acts on root. Drag to move.

**FileTree** — all folders (alphabetical, case-insensitive) then all files (same), no manual ordering. Kept current by the watcher.

**Editor** — full-viewport CodeMirror 5 between TabBar and Footer; word wrap + line numbers, syntax modes by extension. Shows "no open files" when there are no tabs.

**Footer** — undo/redo/word-wrap on the left, length/lines/language on the right. Hidden when there are no tabs.

**cid / Subscription** — the server assigns a cid on WS connect; REST file fetches pass `?cid=`, which subscribes that connection to the file (replacing its previous subscription). Content broadcasts go only to subscribers (excluding the sender). Re-established on reconnect.

**Client prefs** — word wrap, sidebar open/width live in the browser's `localStorage`; nothing is persisted server-side.
