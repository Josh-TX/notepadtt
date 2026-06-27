# Mobile Layout and Sidebar Polish

## Description

I want a series of mobile layout fixes and sidebar/filetree UI improvements.

**App shell removal and layout fix.** The `app-shell` wrapper div should be removed. The sidebar should always use `position: fixed` (not just on mobile), and the desktop "push" behavior is achieved via `margin-left: 400px` on `.main-layout` when the sidebar is open. `.main-layout` uses `height: 100%` (inheriting from `#app`) rather than `height: 100vh`. On mobile, `100vh` combined with the soft keyboard could cause the navbar to scroll off the top of the viewport; the fixed-height inheritance chain prevents this.

**Sidebar slide animation.** The sidebar should animate in and out over 150ms using Vue's `<Transition>` component with a `translateX(-100%) → 0` CSS transition. On desktop the `margin-left` on `.main-layout` transitions simultaneously at the same duration so the content push is in sync.

**Mobile overlay.** On mobile (≤767px), when the sidebar is open, a semi-transparent dark overlay (`rgba(0,0,0,0.4)`) should appear behind the sidebar but above the main layout (`z-index: 50`, sidebar at `z-index: 100`). Tapping the overlay closes the sidebar. The overlay fades in/out over 150ms matching the sidebar animation.

**Sidebar collapse button.** The close button (✕, top-right of sidebar) should be replaced with a collapse button in the top-left of the sidebar header. The button uses a double left chevron SVG (⟨⟨) to indicate "collapse". This ensures there is always a sidebar toggle button visible in the top-left corner of the viewport regardless of sidebar state — the navbar hamburger when closed, the sidebar's collapse button when open. The sidebar header height should match the navbar (35px) so they align visually.

**FileTree root folder row.** The FileTree should display a "root" folder row at the very top of the sidebar's file tree. This root row behaves exactly like any other folder: it can be expanded/collapsed with a chevron, and right-clicked to get the folder context menu. The root starts expanded when the sidebar opens. The API data structure is unchanged — this is purely a UI addition via a `label` prop on the FileTree component. When `label` is provided, the component renders a folder row for itself before rendering its children list.

**FileTree styling.** Folder expand/collapse icons should use SVG chevrons (right when collapsed, down when expanded) instead of unicode triangle characters. File rows should have no icon at all — the absence of a chevron is sufficient to distinguish files from folders. The `tree-list` indent is 18px. File rows get additional left padding and margin to visually distinguish them from folder rows.

**Font size standardization.** Most explicit `font-size` declarations should be removed from components, letting the root body size (13px) apply by default. Exceptions: the "NTT" brand text stays at 18px, the CodeMirror editor stays at 14px, the navbar "+" new-file button is 20px, footer action buttons are 14px, and the footer's right stats section is 14px.

**Same-folder file click bug fix.** Clicking a file in the FileTree that is already in the current folder did nothing, because the sidebar set `pendingFileId` and relied on a `router.push` to trigger the route watcher — but same-route navigation is a no-op in Vue Router. The fix: when the target file's folder matches the current folder, directly fetch and activate the file instead of going through the router.
