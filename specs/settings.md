# Settings

## Description

I want a new Settings feature: a single persisted, app-wide settings record that the UI loads on every refresh and that various components can read from (and, for some fields, later act on). This is the user-configurable home for the retention values that `file-version-retention.md` and `file-trash.md` explicitly called out as "hardcoded for now, planned for later," plus a handful of new UI-preference settings, some of which are inert placeholders for a future feature.

### Storage

A new `Settings` table holding exactly one row, following the existing wide-table style used elsewhere (`files`, `FileVersions`) rather than a JSON blob — one column per setting. The row is seeded with defaults at schema-creation time (same "no migration needed, dev DB gets recreated" approach used by prior specs) so the table is never empty.

Fields, with defaults:

- `TabMode`: `normal` | `folder` — default `folder`
- `TabCloseIcon`: `visible` | `hidden` | `new` (visible only on tabs named like `new N`) — default `new`
- `WordWrapMode`: `device-on` | `device-off` | `file-on` | `file-off` — default `device-on` ("Initially on per device, persists")
- `SearchHistory` (bool) — default `false`
- `SearchTrash` (bool) — default `false`
- `SearchRegex` (bool) — default `false`
- `LinesPerResult` (int) — default `4`
- `MaxResultsPerFile` (int) — default `2`
- `TrashTTL` (duration string) — default `30d` (this is an intentional change from today's hardcoded `30m` constant in `retention.go`)
- `ShortTermTTL` — default `10m`, `ShortTermMinDelay` — default `40s`
- `MedTermTTL` — default `120m`, `MedTermMinDelay` — default `8m`
- `LongTermTTL` — default `7d`, `LongTermMinDelay` — default `18h`
- `VeryLongTermTTL` — default `60d`, `VeryLongTermMinDelay` — default `5d`

The 8 retention defaults match today's existing Go constants exactly; only `TrashTTL`'s default actually changes (30m → 30d).

### Backend wiring for the retention settings

`TrashTTL` and the 8 File History TTL/MinDelay values stop being hardcoded Go constants and become live settings: an in-memory cache of the current `Settings` row is loaded at startup and refreshed on every successful save. The FileVersioning process (its TTL sweep, its `FileTrash` sweep, and its MinDelay-based Term assignment) reads from this cache on every run, so a saved change takes effect on the *next* run (≤1 minute later) — no server restart required. The existing `mustParseDuration`-style parsing (case-insensitive `s`/`m`/`h`/`d` units, optional space) is reused for parsing these fields out of the cache.

All other settings (`TabMode`, `TabCloseIcon`, `WordWrapMode`'s mode selector, the search filter fields) are simply stored and returned by the API in this spec — no backend behavior reads them yet, aside from the Word Wrap wiring described below.

### API

- `GET /api/settings` — returns the current settings row as JSON. Called by the frontend once on app load, alongside the existing file-tree fetch.
- `PUT /api/settings` — accepts the full settings object and replaces the row atomically (the modal has one Save button that submits every field at once; there's no per-field PATCH). Validation, in order:
  - Each duration string (`TrashTTL` + the 8 retention fields) must match the existing duration format (`\d+` optionally followed by a space, then one of `s`/`m`/`h`/`d`, case-insensitive). Zero is allowed (e.g. `0s`) — no minimum is enforced.
  - The 4 TTLs (`ShortTermTTL` < `MedTermTTL` < `LongTermTTL` < `VeryLongTermTTL`) must be strictly increasing, and the 4 MinDelays must independently be strictly increasing too — this is the invariant the FileVersioning process's cascading-tier logic depends on. (Deliberately *not* validated: a tier's MinDelay against its own TTL — e.g. `ShortTermMinDelay >= ShortTermTTL` is left as an unenforced edge case, even though it could in theory produce a version that's already past its own expiry the moment it's created.)
  - `LinesPerResult` and `MaxResultsPerFile` must be positive integers (≥1) — no upper bound.
  - On any failure, the whole request is rejected (no partial apply) with an error; nothing in the in-memory cache or DB changes.
  - On success, the DB row and in-memory cache are both updated, and the response body is the saved settings object.

### Frontend

- `store.js` gains a `settings` field in the reactive state, populated by a `getSettings()` call made in `App.vue`'s `onMounted` alongside the existing `getFiles()` tree fetch. A `saveSettings(newSettings)` function PUTs to the backend; on success it writes the response straight into `store.settings` so the editing client applies changes immediately (no WS broadcast — other open tabs/clients only see the change on their next page load, consistent with how the tree fetch already works). On failure, it shows the existing toast (`showToast`) with one generic message, even when multiple fields are invalid — not an itemized, field-by-field list.
- `Sidebar.vue` gains a new footer section, a sibling below the existing `sidebar-scroll`/FileTree div (today's `search-row`/`trash-row` live at the *top* of the sidebar, above the FileTree — this is a distinct new bottom area). It contains one "Settings" row styled identically to `search-row`/`trash-row` (same flex/padding/hover treatment, gear/cog icon) that opens a new SettingsModal via an `openSettingsModal()` store function, mirroring `openSearchModal`/`openTrashModal`.
- `store.js` gains `settingsModalOpen` plus `openSettingsModal()`/`closeSettingsModal()`, following the exact pattern already used for the other three modals.
- A new `SettingsModal.vue`, `Teleport`'d to body like the others, full-screen on mobile / large centered on desktop (matching `SearchModal`/`TrashModal`). One scrollable form with these sections, in this order: Tab Mode, Tab Close Icon, Word Wrap, Initial Search Filters (3 checkboxes + the 2 number fields), Trash TTL, File History (the 4 TTL/MinDelay pairs). A single Save button submits the whole form as one `PUT`; on a validation error the toast appears and the modal stays open with the user's edits intact; on success the modal closes.

### Word Wrap behavior

This setting actually changes today's behavior, replacing the flat, session-only `store.wordWrap` boolean:

- `WordWrapMode` itself (which of the 4 strategies is active) is the one part of this that lives in the `Settings` table.
- For `device-on`/`device-off`: the actual on/off boolean is per-device, stored in `localStorage` (never in the `Settings` table, since a single shared row can't represent per-device values) and persists across reloads. On a device with no stored value yet, it initializes from the mode's on/off default.
- For `file-on`/`file-off`: the actual on/off boolean lives in a frontend in-memory map keyed by FileId, scoped to the current session only (lost on reload, matching today's lack of persistence). Each file starts at the mode's on/off default the first time it's opened in a session; toggling the Footer's existing wrap button from then on only changes that file's entry.
- `Editor.vue`'s `lineWrapping` option and the Footer toggle button read/write through whichever of the two resolution strategies above is currently active, instead of a flat `store.wordWrap`.

## Out of Scope

- Any actual behavior change for `TabMode`, `TabCloseIcon`, or the 5 search-filter fields (`SearchHistory`, `SearchTrash`, `SearchRegex`, `LinesPerResult`, `MaxResultsPerFile`) — these are stored and editable via the modal now, but `TabBar.vue`/`SearchModal.vue`/`search.go` are unchanged in this spec. They're wired up by a later feature.
- Live cross-client sync of settings changes (WebSocket broadcast) — other open tabs only pick up a change on their next page load.
- A "reset to defaults" action in the Settings modal.
- Per-tier validation of a MinDelay against its own TTL.
