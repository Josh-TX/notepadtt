# Move File Modal

## Description

I want a "Move" action added to the context menus for tabs (TabBar) and FileTree file/folder rows. It appears immediately after "Rename" in both menus. When triggered, it opens a new MoveFileModal. If invoked on a file the title is "Move File", if on a folder "Move Folder". The subtitle (or just part of the title) shows the original full path — format the header as "Move \<original-path\>".

The modal has a single-line text input at the top pre-filled with the item's full current path. To its right is a "Move" button that executes the move and closes the modal. Pressing Enter in the input is equivalent to clicking Move. The Move button is disabled when the input path equals the item's current path (no-op), or when the destination path conflicts with an existing file/folder (detected in real-time against the in-memory FileTree, no server round-trip). When the button is disabled due to a conflict, an inline error message appears below the input row (small red text). No inline error is shown for the no-op case.

Below the input row are breadcrumbs, and below those is a folder browser. Together they help navigate the destination directory without typing. The breadcrumbs reflect the directory portion of the current input value (everything up to and including the last `/`), using "/" as the root label. Clicking a breadcrumb segment navigates to that ancestor directory. The folder browser shows all immediate subfolders of that same directory. Clicking a subfolder traverses into it: the directory portion of the input updates to that subfolder's path, while the filename/foldername at the end of the input stays unchanged. When the current directory has no subfolders, the browser shows muted text: "no subfolders". The modal opens with the folder browser seeded at the item's current parent directory.

The Move button should work even when the typed destination path refers to a directory that doesn't exist yet — the backend must create any missing parent folders automatically (MkdirAll) before moving the item. This is how a user creates a new folder: they simply type a new path segment and click Move.

On the backend, the existing `PUT /api/files/{id}/move` endpoint is updated to run MkdirAll on the destination directory before moving, so typing a novel folder path succeeds instead of erroring. The move-file endpoint's existing auto-rename-on-conflict behavior (NextDuplicateName) should be removed — conflicts are caught client-side and the button is disabled, so the server should return a 409 if it somehow receives a conflicting path. The existing `PUT /api/folders/move` endpoint similarly needs MkdirAll support; its existing 409-on-conflict behavior is already correct.

After a successful file move, the same navigation/tab-reopening behavior used by the existing drag-to-move feature applies (navigate CurrentFolder to the new location, reopen the tab). Folder moves follow the same post-move behavior as the existing drag-to-move for folders.

## Out of Scope

- Any filter on the folder browser list (no search/startswith filtering of subfolders)
- An explicit "New Folder" button in the modal body
- Moving multiple items at once
