# FileTree Drag Move

## Description

I want desktop users to be able to move files and folders within the Sidebar's FileTree by dragging them onto other folders. This is desktop-only (excluded on touch devices via the same `isTouchDevice` check used elsewhere) — no non-drag fallback (e.g. a "Move to..." menu item) is being added. The drag source is limited to rows inside the FileTree itself; dragging a TabBar tab onto a Sidebar folder is not supported.

Both files and folders are draggable, and both can be dropped onto folders to relocate them (a folder move relocates its whole subtree).

### Drop target resolution

- Hovering a **folder's own row** makes that folder the drop target.
- Hovering a **file row** makes that file's parent folder the drop target (equivalent to hovering the parent folder itself).
- The FileTree's **root row** (labeled "root") is a valid drop target representing the top-level folder. Blank space below the last rendered row, but still inside the Sidebar, also resolves to root — a forgiving target so users don't need to hit a precise pixel.
- Anywhere outside the FileTree (Navbar, TabBar, Editor, Footer, or outside the Sidebar entirely) is not a valid drop target. Dropping there cancels the move with no API call — this falls out naturally from HTML5 drag-and-drop since no drop listeners are attached outside the FileTree.

### Visual feedback

- While hovering a valid drop-target folder, show an inset box-shadow (a border that takes no layout space) plus an extremely subtle background tint, using colors consistent with the existing theme (e.g. the blue accent `#0078D4` for the inset border, a faint tint between the existing hover color `#2a2d2e` and the base background). Exact values are not prescribed beyond "subtle" and theme-consistent.
- The row being dragged is dimmed to `opacity: 0.4`, matching the existing convention in TabBar's drag-and-drop reordering. Rely on the browser's default drag-ghost image (no custom drag image).
- Invalid targets (see Folder-into-itself below) show no highlight at all — they're treated as if not a folder.

### Hover-to-expand

The only hover target that matters for auto-expand is a folder's own row (not its rendered descendants). If a collapsed folder's row is hovered long enough during a drag, it auto-expands so the user can drill deeper without first manually expanding it. The dwell time required is shorter when the pointer stays nearly still over the row, and longer when the pointer keeps moving (but stays within that row). Both durations should be named constants (e.g. ~500ms for "still", ~1000ms for "moving") so they're easy to tune later — exact values aren't critical up front.

### Auto-scroll

While dragging near the top/bottom edge of the scrollable FileTree, auto-scroll it vertically, following the same edge-zone speed-ramp pattern already used by TabBar's horizontal auto-scroll (its `SCROLL_ZONE` / `MAX_SCROLL_SPEED`-style constants).

### Invalid moves and conflicts

- Dragging a folder onto itself or onto any of its own descendant folders is invalid (would create a cycle). No hover highlight is shown for that target during drag, and the backend independently validates and rejects this defensively (don't rely solely on frontend prevention).
- Dropping a file or folder onto its own current parent folder is a no-op — detect this client-side and skip the API call entirely; OrderNum/position stays unchanged.
- **Folder name conflicts**: if a folder with the same name already exists at the destination, the move fails. Validate this on both frontend and backend.
- **File name conflicts**: if a file with the same name already exists at the destination, auto-rename using the existing `NextDuplicateName` logic (the same one used by Duplicate and Trash-restore), landing on "(2)" etc., rather than failing.

### Ordering and post-move behavior

- A moved file is appended at the end of its destination folder (`Max(OrderNum)+1`), the same rule used for newly created or restored files.
- After a successful file move, auto-open the file in the Editor and navigate CurrentFolder/breadcrumbs to the file's new destination folder — mirroring Trash-restore's existing "open after action" behavior exactly.
- If a folder move changes the path of the currently-viewed CurrentFolder (because CurrentFolder is the moved folder itself, or a descendant of it), recompute CurrentFolder's new path using the same prefix-replace logic as `UpdateFolderPath`, so breadcrumbs stay valid and follow the relocated folder rather than breaking or silently resetting to root.

### Frontend/backend interaction pattern

Follow the same convention as the existing TabBar drag-and-drop reorder feature: the frontend issues the move API call and waits for the resulting WebSocket FileTree broadcast to update the rendered tree, rather than optimistically mutating local state before the server confirms. Unlike the existing reorder call (which is unguarded), wrap the new move API calls in try/catch and surface failures via the existing `showToast(..., 'error')` pattern.

### API

Two new routes, parallel to the existing rename routes but taking a full new path instead of just a new name:

- **Move file**: similar to the existing `PUT /api/files/{id}` rename route, but takes a full new path (including filename) instead of just a `name`. The backend resolves the file's current path via FileId, validates the destination with `jailPath()`, performs the disk rename, updates the DB Path column (`UpdatePath`), and broadcasts the updated tree — same plumbing as rename. If the destination has a name conflict, the backend resolves it via `NextDuplicateName` instead of rejecting.
- **Move folder**: similar to the existing `PUT /api/folders` rename route, but takes a full new destination path instead of just a `name`. The backend validates that the new path is not the same as, or a descendant of, the old path (reject if so), and that no name conflict exists at the destination (reject if so) — both checks happen server-side even though the frontend already prevents them via the drag UI. On success, performs the disk rename and updates the DB via the existing `UpdateFolderPath` prefix-replace cascade, then broadcasts the updated tree.

## Out of Scope

- Mobile/touch support for this drag-and-drop feature, and any non-drag fallback (e.g. a "Move to..." context-menu item with a folder picker) for touch users.
- Dragging a TabBar tab onto a Sidebar folder (cross-component drag).
- Inserting at a specific position when dropping on a file row — dropping on a file always resolves to "move into its parent folder, appended at the end," never an ordered insert.
- Overwriting an existing file/folder at the destination on conflict — folder moves reject on conflict, file moves auto-rename instead.
