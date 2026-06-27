# Sidebar Resize

## Description

I want two new persisted settings — `EditorFontSize` and `SidebarWidth` — plus a third, `DesktopSidebarOpen`, and a draggable, resizable Sidebar to go with `SidebarWidth`.

### EditorFontSize

This is a normal addition to the existing `Settings` table/struct/modal, following the exact same pattern as fields like `TabCloseIcon` or `LinesPerResult`:

- New `EditorFontSize` int column on the `Settings` table, default `14` (matches the previously hardcoded `font-size: 14px` in `Editor.vue`).
- Edited via a new number input in `SettingsModal.vue`, alongside the existing fields, using the same input styling/conventions already used for `linesPerResult`/`maxResultsPerFile`/`maxFiles`.
- Saved through the existing full-form `PUT /api/settings` flow — no dedicated endpoint. It's just another field bundled into `saveSettings(form.value)`.
- Bounds: 10–24 (px). The backend silently clamps any out-of-range value into `[10, 24]` via a shared `clamp(v, lo, hi)` helper rather than rejecting the save (consistent with `SidebarWidth` below — clamp, don't reject).
- No live preview while the modal is open — like every other setting in the modal, it only takes effect in the Editor after Save succeeds and `store.settings` is updated. A `watch` on `store.settings.editorFontSize` reapplies the CodeMirror theme and calls `cm.refresh()` so line heights/widths are re-measured.
- Applied to `Editor.vue`'s CodeMirror instance (the font-size CSS previously hardcoded). Scope is just the main Editor — HistoryModal's content viewer and SearchSnippet have their own independent styling per the glossary and are unaffected.

### SidebarWidth

Unlike `EditorFontSize`, this field is never exposed in the Settings modal at all. It still lives on the same `Settings` table/struct (so it round-trips through `GET`/`PUT /api/settings` like every other field), but it's updated exclusively through a dedicated endpoint, mirroring how `WordWrap` already has its own narrow `PUT /api/settings/wordwrap` endpoint separate from the full-form save:

- New `SidebarWidth` int column on `Settings`, default `400` (matches the previously hardcoded `width: 400px` in `App.vue`'s `.sidebar`/`.main-layout.sidebar-open` CSS).
- New endpoint: `PUT /api/settings/sidebarwidth`, body `{ "sidebarWidth": <int> }`, response echoes the saved value — same shape/response convention as `handleUpdateWrap`/`PUT /api/settings/wordwrap`. The handler updates just the `SidebarWidth` column (`UpdateSidebarWidth` DB method, mirroring `UpdateWordWrap`) and the in-memory settings cache (`setSidebarWidthCache`, mirroring `setWordWrapCache`), bypassing `validateSettings`'s full-form validation.
- Bounds: clamped to `[100, 600]` server-side via the same `clamp` helper (silently clamp, do not reject).
- Frontend mirrors the `WordWrap` dual-state pattern: a standalone `store.sidebarWidth` field (default `400`), populated from `settings.sidebarWidth` at startup (see "Settings load timing" below) alongside `store.wordWrap`. The drag handler updates `store.sidebarWidth` optimistically and calls the new endpoint directly — it never touches `store.settings`. `SettingsModal.vue`'s form-snapshot folds in `sidebarWidth: store.sidebarWidth` (alongside `wordWrap` and `desktopSidebarOpen`, see below) so that a full-form Settings Save still round-trips the most recent dragged width instead of reverting it to a stale `store.settings` snapshot.
- Excluded entirely from `SettingsModal.vue` — no input, no label, nothing in the template for it.

### Resizable Sidebar UI

The Sidebar is drag-resizable, but only when both of the following hold:

1. **Push-layout mode** — i.e. not the mobile overlay variant. A shared reactive flag, `store.isPushLayout`, is set up in `store.js`'s `initLayoutFlags()` (called once from `main.js` before mount) via `matchMedia('(min-width: 768px)')` with a `change` listener, reusing the same `768px` breakpoint already baked into the CSS. This is the single source of truth for any JS-level "are we in push-layout mode" check.
2. **Non-touch / has a cursor** — `Sidebar.vue` checks `window.matchMedia('(pointer: coarse)').matches` once on mount, exposed as a local `isTouchDevice` ref (same detection convention `TabBar.vue` already uses).

The resize-handle element itself is rendered with `v-if="store.isPushLayout && !isTouchDevice"` — when either condition is false, the handle doesn't exist in the DOM at all.

**Handle appearance**: a 6px-wide invisible hit-area (`position: fixed`, full viewport height) running along the Sidebar's right edge, positioned via an inline `left` style derived from `store.sidebarWidth`. At rest it shows no visual indicator. On hover it shows a neutral `#2d2d2d` background; while actively dragging, the handle itself goes transparent and the accent highlight instead appears as a `1px solid #0078D4` border on the Sidebar's own right edge (toggled via a `resizing` class bound to `store.sidebarDragging`). Cursor is `col-resize` while hovering the handle.

**Drag mechanics**: implemented with Pointer Events (`pointerdown`/`pointermove`/`pointerup`) with explicit pointer capture, not HTML5 drag-and-drop (unlike the tab-reorder feature) — this is a continuous resize, not a discrete reorder. While dragging:
- `store.sidebarDragging` is `true`; both the Sidebar's width and the main layout's `margin-left` (computed in `App.vue` from `store.sidebarWidth`) update live, tracking the cursor.
- The normal `150ms ease` transition on the main layout's `margin-left` is suspended for the duration of the drag — `App.vue`'s `mainLayoutStyle` computed sets an explicit inline `transition: none` whenever `store.sidebarDragging` is true, falling back to the CSS-defined transition (`undefined`) otherwise.
- The dragged width is clamped client-side to `[100, 600]` px live (`Math.min(600, Math.max(100, next))`), so the handle can't be dragged past the limits even before any network round-trip.
- No network call happens during the drag itself.

**On release** (`pointerup`): exactly one `PUT /api/settings/sidebarwidth` call fires with the final width. `store.sidebarWidth` was already updated optimistically during the drag, so this is just persistence. If the call fails, `store.sidebarWidth` reverts to the last known-good (pre-drag) value and an error toast is shown — same revert-on-failure pattern `Footer.vue`'s word-wrap toggle already uses.

**CSS**: the previously-hardcoded `400px` literals in `App.vue`'s `.sidebar` width and `.main-layout.sidebar-open` margin-left are now dynamic, bound to `store.sidebarWidth` via computed inline styles (`sidebarWidthStyle`/`mainLayoutStyle`), applied only when `store.isPushLayout` is true. The mobile overlay width (`85vw` / `max-width: 400px`) is unaffected — it stays exactly as before, since resizing is push-layout-only.

### DesktopSidebarOpen

A third new persisted setting, added so the desktop (push-layout) sidebar's open/closed state survives a page reload — previously it always reset to closed. Same dedicated-endpoint pattern as `SidebarWidth`/`WordWrap`, and likewise never exposed in the Settings modal:

- New `DesktopSidebarOpen` bool column on `Settings`, default `false`.
- New endpoint: `PUT /api/settings/desktopsidebaropen`, body `{ "desktopSidebarOpen": <bool> }`, response echoes the saved value. Handler (`handleUpdateDesktopSidebarOpen`) updates just the `DesktopSidebarOpen` column (`UpdateDesktopSidebarOpen` DB method) and the in-memory cache (`setDesktopSidebarOpenCache`), bypassing full-form validation — mirrors `handleUpdateWrap` exactly.
- Persisted from two places, both only while `store.isPushLayout` is true (on mobile the toggle/collapse stays purely visual, no network call):
  - **Navbar's sidebar toggle button** (`onToggleSidebar`): flips `store.sidebarOpen` immediately via the existing `toggleSidebar()`, then persists the new state. On failure, toggles back and shows an error toast.
  - **Sidebar's collapse button** (`onCollapseClick`): closes via the existing `closeSidebar()`, then persists `false`. On failure, reopens (`store.sidebarOpen = true`) and shows an error toast.
- `SettingsModal.vue`'s form-snapshot folds in `desktopSidebarOpen: store.sidebarOpen` (alongside `wordWrap` and `sidebarWidth`) so a full-form Save round-trips the live open/closed state instead of a stale one.

#### Settings load timing

Loading settings (previously done in `App.vue`'s `onMounted`) moved to `main.js`, awaited before `createApp(App).mount('#app')`. This is required specifically for `DesktopSidebarOpen`: applying it post-mount (in `onMounted`) would make the Sidebar's open/close `<Transition>` see `sidebarOpen` flip after the initial render and animate the slide-in on every page load, instead of just rendering already-open. `store.settings`, `store.wordWrap`, and `store.sidebarWidth` are now also populated at this same pre-mount point for consistency, and `store.sidebarOpen` is only overridden from `settings.desktopSidebarOpen` when `store.isPushLayout` is true.

### Migration note

There's no schema migration mechanism in this codebase (`createSettingsSchema` only does `CREATE TABLE IF NOT EXISTS` plus a seed-if-empty check) — adding new columns to an existing on-disk DB file would break `GetSettings`/`SaveSettings`'s column-count-matched scans. Per existing project convention, no migration is added for `EditorFontSize`, `SidebarWidth`, or `DesktopSidebarOpen` — the DB is simply deleted/recreated during development, same as before.

## Out of Scope

- A maximum-width cap on anything other than the main Sidebar (e.g. modal sidebars like HistoryModal's are untouched).
- Any live preview of `EditorFontSize` while the Settings modal is open.
- A "reset to default width" action for the Sidebar.
- Cross-client sync of any of the three new settings via WebSocket — same as all other settings today, other open tabs only pick up a change on their next page load.
