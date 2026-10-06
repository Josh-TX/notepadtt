# notepadtt rewrite — decisions

Drastic simplification: stateless server, filesystem is the only persistent store.

## Removed
- SQLite DB (`.notepadtt.db`, FTS5), all persistent storage
- FileTrash / TrashModal, FileVersion / HistoryModal, FileVersioning process, Terms
- Settings modal
- Navbar, CurrentFolder, breadcrumbs
- OrderNum, manual FileTree ordering
- Sidebar Search/Trash/Settings rows
- Footer History button

## Scope & config
- Everything confined to data root. Paths absolute but under root.
- CLI flags unchanged: `-d|-directory` (default `.`), `-p|-port` (default 8080). Fixed for server lifetime.
- Globally excluded (tree, watcher, search; treated as nonexistent): `.git`, `node_modules`, `.ntt-trash`. All other files shown, incl. dotfiles.

## Identity & sync
- FileId kept: in-memory map, assigned on open, survives renames while server runs. Path is an attribute.
- VersionId + RecentFileVersions kept, in-memory only (5s purge, no FileVersioning step). Conflict merge unchanged.
- Server writes to disk on every edit message, immediately. No save button, no dirty state.
- fs watcher on open files: external changes go through the same merge path as another client's edit. Watcher must ignore server's own writes (compare content/hash vs last written).
- Tree watcher: recursive over root (minus exclusions), pushes tree changes live to all clients.
- File deleted externally while open: tab closes in all clients, silently. (External rename = same, tab closes.)
- Content held in memory only while ≥1 client has it loaded.

## Tabs
- No "selected folder". Tabs may be from any directory. Tab label = filename; hover title = full absolute path.
- Close button always visible on every tab. Closing never deletes the file.
- "+" new-file button lives in TabBar.
- Server holds open-tab list in memory, shared live across all clients (open/close/reorder). Active tab is per-client; server stores last-set active tab only to give new connections.
- No tabs open at server start. State lost on restart. Manual tab reorder allowed, lost on restart.
- Opening a file (sidebar/search): append at end; if already open, activate.
- Closing active tab: activate right neighbor, else left, else empty state.
- Empty state: centered muted "no open files"; Footer hidden/disabled.
- Tab right-click menu kept (minus History).

## New files
- Created in data root, named `new N` (no extension), lowest unused N checked against disk.

## Large / binary files
- Over 1000 KB: tab shows message + "display anyway" button. Per-client choice (other clients still see message until they click).
- Binary (server detects, e.g. NUL byte in first chunk): message + Download button only. No display-anyway, no editing.

## Delete / undo
- Applies to files and folders (folder moves whole).
- Server moves target to `.ntt-trash` in data root (same filesystem so rename works) for 5s, then deletes.
- Tabs of deleted files close in all clients.
- Only the deleting client sees toast with UNDO. Others just see file vanish.
- UNDO restores file (and tabs for it, at same position). If original path now occupied: restore fails, toast "can't restore: path exists", trash copy kept until 5s expiry.
- `.ntt-trash` wiped on server startup (crash during 5s window = permanent loss, accepted).

## Search
- Ctrl+F: browser-native find. Editor already uses CodeMirror 5 `viewportMargin: Infinity` (`Editor.vue:37`) — keep.
- Search Modal kept. Server shells out to `grep`/`rg` over root per query; top 10 results with snippets; filename matches too.
- OPEN: UI entry point for Search Modal (Sidebar Search row removed).

## FileTree / Sidebar
- Folders first, then files; each case-insensitive alphabetical. No manual ordering.
- Sidebar = FileTree only (no Search/Trash/Settings). Keep right-click context menus on whole sidebar.

## Editor / Footer
- Editor unchanged (CodeMirror 5, word wrap, line numbers).
- Footer unchanged minus History button.

## Notes / conflicts flagged
- Repo used SQLite, not MySQL; all DB-dependent glossary terms (Term, FileVersion, FileTrash, OrderNum, CurrentFolder, Navbar, HistoryModal, TrashModal) need removal/update in CLAUDE.md and GLOSSARY.md.
