# notepadtt Initial Implementation

## Description

I want to build a web-based text file editor called notepadtt. It's a Go server that serves a Vue 3 SPA (built with Vite) embedded in the binary via `embed.FS`. The Go module name is `github.com/Josh-TX/notepadtt`.

### Backend

The server is written in Go. By default it serves the current working directory as the file root, configurable via `-d` or `--directory` flags. The port defaults to 8080, configurable via `-p` or `--port`. The server enforces a hard path jail — no file access outside the root is permitted.

The backend maintains a SQLite database named `.notepadtt.db` in the current working directory. The `files` table has four columns: `FileId` (random UUID, primary key), `Path` (full relative path from root, e.g. `subdir/foo.txt`), `Content` (full text content), and `LastOpened` (timestamp, updated whenever a client opens the file).

SQLite is the primary source of truth. On every write from a client, content is immediately flushed to both SQLite and disk (no debouncing). On startup, if a file on disk has a newer mtime than what's in SQLite, disk content wins and SQLite is updated.

The backend scans for files with these extensions: `.txt`, `.md`, `.json`, `.yaml`, `.sh`. Additionally, any file already tracked in SQLite (has a FileId) is served regardless of extension — this handles files renamed to non-allowlisted extensions. Files named `new N` (where N is an integer) are also served regardless of extension.

The backend uses `fsnotify` (or equivalent) to watch the filesystem for external changes while running. New files matching the allowlist get a new FileId and are inserted into SQLite immediately. When a file is renamed, the backend updates the `Path` column for the existing FileId using the old+new path from the fsnotify rename event — the FileId is preserved. External filesystem change events trigger a broadcast of the full FileTree to all connected WebSocket clients. REST mutations (create, rename, delete) also trigger this broadcast directly — they do not wait for fsnotify to pick up the change.

### Database & File Identity

All logic throughout the project is FileId-centric. The file's name/path is a secondary display detail. If user A is editing a file and user B renames it, user A's editor is unaffected except that their tab updates its displayed name — because everything references FileId, not path.

### WebSocket Protocol

One WebSocket connection per client. Messages are JSON.

**Connection init:** On connect, the server immediately sends `{ type: "init", cid: "<uuid>" }`. The client stores this `cid` and includes it as a `?cid=` query param on REST calls that affect subscription state.

**Subscription:** The server tracks one active file subscription per connection (`cid → fileId`), held in memory only — not persisted to SQLite. All subscriptions are cleared on server restart; all cids are cleared on client disconnect. A subscription is established as a side-effect of `GET /api/files/:id?cid=...` — that call both returns file content and registers the subscription. One subscription per client at a time; fetching a new file replaces the prior one. On WebSocket reconnect the client receives a new `cid` and re-fetches the active file, re-establishing its subscription.

**Tab switching:** The client always re-fetches `GET /api/files/:id?cid=...` on every tab switch, even if the content is already in memory. This keeps content fresh and re-registers the subscription in one call.

**Client → Server:** `{ fileId: string, content: string }` — sent on every keystroke with the full file content. The server writes immediately to SQLite and disk, then broadcasts `{ type: "content", fileId, content }` to all other clients currently subscribed to that fileId (not back to the sender). Last-write-wins for concurrent edits.

**Server → Client:**
- `{ type: "init", cid: string }` — sent immediately on connect.
- `{ type: "filesystem", tree: <FileTree> }` — sent to all connected clients whenever the filesystem changes. The frontend waits for this broadcast before updating the FileTree UI; there are no optimistic updates from REST responses.
- `{ type: "content", fileId: string, content: string }` — sent to clients subscribed to the edited fileId, excluding the sender.

### REST API

Initial data is loaded via HTTP, not WebSocket:
- `GET /api/files` — returns the full FileTree
- `GET /api/files/:id?cid=<cid>` — returns file content for a FileId; as a side-effect, registers the client's subscription to that file

Mutations:
- `POST /api/files` — create a new file (body: `{ parentPath, name }`)
- `PUT /api/files/:id` — rename a file (body: `{ name }`)
- `DELETE /api/files/:id` — delete a file permanently (no trash)
- `POST /api/files/:id/duplicate` — duplicate a file
- `POST /api/folders` — create a new folder (body: `{ parentPath, name }`)
- `PUT /api/folders` — rename a folder (body: `{ path, name }`)
- `DELETE /api/folders` — delete a folder recursively (body: `{ path }`)

### Build

A `Makefile` is used. `make build` runs `vite build` in the `frontend/` directory, then `go build` which embeds the built frontend via `embed.FS`.

---

### Frontend

The frontend is Vue 3 + Vite, located in the `frontend/` subdirectory. It uses Vue Router, with the current folder path reflected in the URL (e.g. `/#/folder/subfolder`) so locations are bookmarkable.

The editor uses **CodeMirror 5** installed via npm and bundled with Vite. It uses `viewportMargin: Infinity` so the browser's native Ctrl+F works across the entire document. The only enabled features are word wrap (default on, global toggle) and line numbers. No syntax highlighting, no other themes.

### Layout

The layout has four rows top to bottom: **Navbar**, **TabBar**, **Editor** (fills all remaining space), **Footer**.

On desktop (≥768px), when the Sidebar is open it pushes the entire layout (all four rows) to the right. On mobile (<768px) the sidebar is a fixed-position overlay that slides in from the left without pushing anything.

**Navbar:** Topmost bar. Left edge: sidebar toggle button (static icon, not state-dependent). Then "NTT" label (not a link), followed by breadcrumb trail showing the path to CurrentFolder. Right edge: "+" button to create a new file in CurrentFolder. On desktop, all breadcrumbs except the rightmost are clickable links. On mobile, no breadcrumbs are clickable.

**TabBar:** Sits flush below the Navbar. Full-width horizontally-scrollable strip containing one tab per text file in CurrentFolder. No buttons — sidebar toggle and "+" live in the Navbar. When a new tab opens it auto-scrolls into view. When CurrentFolder has no text files, shows centered muted text: "no text files in this folder".

Each tab shows the filename (not the full path). The active tab has a `2px solid #0078D4` top border and `#1f1f1f` background. Inactive tabs have `#181818` background. Right-clicking a tab shows a custom context menu with: Delete, Rename, Duplicate, Download.

- **Delete:** Permanently deletes the file with a confirmation prompt.
- **Rename:** Uses `window.prompt()` for input.
- **Duplicate:** Creates a copy in the same folder named `foo (2).txt`, incrementing until an available name is found.
- **Download:** Client-side Blob download of current content, no server round trip.

**Editor:** CodeMirror 5 fills the entire space between TabBar and Footer. Background `#1f1f1f`.

**Footer:** Pinned to bottom. Left side: Undo, Redo, Word-wrap toggle (shows a checkmark unicode char when enabled). Undo and Redo are non-functional placeholders in this initial implementation. Right side: displays `length: X lines: Y` where length is byte count.

### Color Scheme

Styled to resemble VS Code dark theme:
- Selected tab, editor background: `#1f1f1f`
- Unselected tabs, TabBar background, Navbar background, Sidebar background: `#181818`
- Active tab top border: `2px solid #0078D4`

Tabs should be styled to look like VS Code tabs. The FileTree in the sidebar should be styled to look like VS Code's file explorer.

### Sidebar

A collapsible left-side panel containing the FileTree. Desktop: ~400px wide, pushes layout right. Mobile: fixed-position overlay, does not push layout.

The top of the sidebar has a collapse/hide button. Below is the FileTree.

**FileTree behavior:**
- Folders listed before files, both alphabetically. Hidden files (dotfiles) shown.
- Clicking a folder row expands/collapses it in the tree.
- Each folder row has a small "open" button on the right. Clicking "open" navigates to that folder (updates CurrentFolder, TabBar, navbar). On mobile also closes the sidebar.
- Clicking a file navigates to its folder and opens its tab. On mobile also closes the sidebar.
- Right-clicking a folder shows a context menu: New File, New Folder, Delete Folder (recursive with confirmation prompt), Rename Folder (uses `window.prompt()`).
- On startup the tree starts collapsed except for the path to the current file/folder.

### CurrentFolder & Navigation

CurrentFolder is the folder shown in the navbar breadcrumbs. The TabBar only shows files in CurrentFolder. New files are created in CurrentFolder. CurrentFolder is tracked in Vue Router URL.

When navigating to a folder, the tab auto-selected is the file with the most recent `LastOpened` timestamp. If no files exist in the folder, the TabBar shows "no text files in this folder".

If a folder containing CurrentFolder is deleted, navigate up to the nearest surviving ancestor.

### New File Naming

New files are named `new N` where N is the smallest integer not already used by another `new N` file in that folder. Per-folder counter. E.g. if `new 1`, `new 3`, `new 4` exist, the next file is `new 2`. No extension.

## Out of Scope

- Authentication — open access, no login
- Undo/redo functionality (buttons exist in footer as placeholders only)
- Full-text search / FTS5
- Per-file word wrap setting (word wrap is global)
- Conflict resolution for simultaneous edits (last-write-wins)
