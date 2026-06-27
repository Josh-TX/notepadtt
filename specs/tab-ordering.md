# Tab Ordering

## Description

I want to introduce an `OrderNum` concept to control the display order of files within a folder. This affects both the TabBar and the sidebar FileTree. Folders are unaffected — they remain alphabetically sorted. Only files get OrderNum.

### Database

Add an `OrderNum` column (integer, not null) to the SQLite `files` table. It is a 0-based index scoped per folder. The FileTree payload (both HTTP and WebSocket) must include an `orderNum` field on each file node. Files within each folder in the FileTree response must be sorted by OrderNum ascending, replacing the current alphabetical sort.

### OrderNum Assignment Rules

- **New file (POST /api/files):** Assign `Max(OrderNum) + 1` within the target folder, making it the rightmost tab.
- **External file (fsnotify discovery):** Same as above — `Max(OrderNum) + 1` within the folder.
- **Fresh server start (no DB):** Files discovered during initial scan are assigned OrderNums alphabetically within each folder (0, 1, 2… in alphabetical order).
- **Duplicate (POST /api/files/:id/duplicate):** Insert at `original OrderNum + 1`, incrementing all files in the folder with a higher OrderNum.
- **Delete:** After removing a file, recompact remaining files in that folder so OrderNums are contiguous (0, 1, 2…).

### Reorder API

Add a new endpoint: `PUT /api/files/:id/order` with body `{ targetIndex: number }`. The server moves the file to that index within its folder, reassigns all files in the folder to contiguous OrderNums (0, 1, 2…) reflecting the new order, then broadcasts the updated FileTree to all connected clients. No `?cid=` param needed.

### Frontend — Tab Drag-to-Reorder

Tabs in the TabBar support drag-to-reorder on desktop only. Mobile drag is out of scope. Use the HTML5 drag-and-drop API (`draggable` attribute, `dragstart`/`dragover`/`drop` events).

**UX details:**
- Any tab can be dragged, including the active tab.
- While dragging, the dragged tab shows reduced opacity (default HTML5 ghost behavior is acceptable).
- A thin neutral/white vertical line indicator appears between tabs to show the drop target position.
- The TabBar auto-scrolls when the drag pointer approaches the left or right edge.
- On drop, call `PUT /api/files/:id/order` with the resolved `targetIndex`. Wait for the resulting WS filesystem broadcast to update the UI — no optimistic updates.
- If a WS filesystem broadcast arrives during a drag, apply it immediately. No special handling required for the in-progress drag.

## Out of Scope

- Drag-to-reorder on mobile/touch
- OrderNum for folders (folders remain alphabetically sorted)
- Drag reordering in the sidebar FileTree (only TabBar supports drag)
