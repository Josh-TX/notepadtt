# History Modal

## Description

I want a way to view a file's past content. The TabBar context menu and the FileTree context menu both get a new "History" option (TabBar's works on any right-clicked tab, not just the active one — FileTree only ever lists text files, so no filtering is needed there). The Footer also gets a "History" button, placed in the `.left` group immediately to the right of the "Wrap" toggle, styled like the existing footer buttons. The Footer button is always clickable, even for a file with zero history.

All three entry points open the same HistoryModal for the target file. Opening it always issues a fresh fetch — there's no caching across opens like SearchModal has, since version history changes over time (new edits, the FileVersioning process running).

### Backend: `GET /api/files/{id}/versions`

A new endpoint returns every known past version for a file, with content inlined, so the frontend can hold the full list in memory and do all further work (selection, stats, formatting) without more API calls. Response is a JSON array (never `null`, matches the `/api/search` convention of returning `[]`), each element shaped like:

```json
{ "fileId": "...", "path": "...", "content": "...", "versionId": "...", "date": 0, "term": 1, "pending": false }
```

Sourced from two places, merged into one newest-first-by-`date` list:

1. **Persisted rows** from the `FileVersions` table for that `FileId` (`pending: false`).
2. **Eligible-but-not-yet-persisted previews**: entries currently sitting in `RecentFileVersions` for that `FileId` that would become a `FileVersions` row the next time the FileVersioning process runs. Computed on demand, without writing anything to the DB.

For (2), extract the FileVersioning process's per-FileId cascading eligibility walk (the oldest-to-newest loop with the `ShortTermMinDelay`/`Term=2..4` MinDelay checks, currently inline in `retention.go`) into a shared pure function — something like `determineEligibleVersions(lastDates, entries) []FileVersion` — that takes the per-term last-persisted dates and a FileId's `RecentFileVersions` entries and returns the versions that *would* be inserted, with their computed `Term`, but doesn't insert them. The real FileVersioning process calls this function and then inserts everything it returns; the new handler calls the same function and returns its result as preview entries with `pending: true`, no insert. Entries that fail every eligibility check (too fresh relative to `ShortTermMinDelay`) are excluded entirely — they aren't real versions yet, not even tentatively. This means a single run of the eligibility walk for one FileId is shared verbatim between the background process and this on-demand handler, so they can never disagree about what's eligible.

The `pending` flag exists so the frontend can show a *very subtle* visual distinction for preview entries (not yet a real persisted row) versus confirmed ones — subtle enough that it doesn't read as a meaningful state to track, just a faint hint.

Validate the file exists first (404 if not, same as the existing `GET /api/files/{id}` pattern). No pagination — every version for the file is returned in one response.

### Backend: `GET /api/files/{id}` no longer always marks the file opened

Today this handler calls `s.db.UpdateLastOpened(fileId)` unconditionally, then only updates the Subscription `if cid != ""`. Change it so `UpdateLastOpened` also only happens `if cid != ""` — i.e. move it inside that same conditional. Every current caller (`frontend/src/api.js`'s `getFile`) always sends a `cid`, so this doesn't change any existing behavior. It's needed because HistoryModal sometimes has to fetch a file's current content from the server (see "Current Snapshot" below) for a file the user hasn't actually opened, and that peek shouldn't mark the file as opened or create a Subscription. No new query param — the frontend just omits `cid` for this call.

### Frontend: `api.js`

- `getFileVersions(fileId)` → `GET /api/files/{id}/versions`.
- A second function for fetching a file's live content without the tracking side effect — same endpoint as the existing `getFile`, just without the `cid` query param.

### HistoryModal

Built the same way SearchModal is (`<Teleport to="body">`, overlay + Transition, dark theme). Layout: a sidebar on the left listing versions, a read-only content viewer on the right showing whichever version is selected.

**Current Snapshot.** The sidebar always has one entry pinned at the very top, labeled "Current Snapshot" (no timestamp shown for it, just the label plus its stats), and it's the default selection when the modal opens. Its content comes from one of two places depending on whether the target file is the one currently loaded in the Editor:

- If it is the active/open file: a frozen copy of `cm.getValue()` taken at the moment the modal opens. It does not stay in sync with further WebSocket content updates while the modal is open — it's a snapshot, not a live view.
- If it isn't the active file (e.g. History opened from the FileTree, or from a non-active TabBar tab): fetched via the no-track `GET /api/files/{id}` call described above.

**Past versions.** Below Current Snapshot, a flat list of the versions returned by `GET /api/files/{id}/versions`, newest-first by `date`. No grouping by tier, no tier badges — just the subtle `pending` distinction mentioned above. If there are zero past versions, show muted placeholder text (e.g. "No history yet") in that section instead of an empty list — Current Snapshot is still shown and selected.

Each non-Current row shows:
- An absolute, locale-formatted date/time (e.g. via `Date.prototype.toLocaleString`).
- Byte length, computed and displayed exactly like Footer does (`new TextEncoder().encode(content).length`), no KB/MB formatting.
- Line count (`content.split('\n').length`).

**Content viewer.** Read-only, styled to match the Editor/SearchSnippet's dark theme (background `#1f1f1f`, text `#d4d4d4`, monospace font, line-number gutter in `#858585` starting at 1 for that version's own content). Unlike SearchSnippet, lines never wrap and are never truncated — long lines scroll horizontally, and the pane scrolls vertically for arbitrarily long files. Build this as its own markup/CSS inside HistoryModal rather than extracting a shared component with SearchSnippet — the two have different enough requirements (4-line truncated snippet vs. full scrollable file) that sharing isn't worth it.

**Responsive layout.**
- Desktop (≥768px): modal sized like SearchModal's desktop mode. Sidebar is a fixed 300px, not resizable, not collapsible at this breakpoint.
- Mobile (<768px): modal is full-screen — 100vw/100vh, no border-radius — matching SearchModal's mobile mode exactly. A toggle button fixed at the modal's top-left shows/hides the sidebar. Collapsing animates the sidebar's width down to 0 over 150ms (content pane fills the freed width); expanding reverses it. Selecting a version while the sidebar is expanded automatically collapses it afterward so the content pane has room. The toggle button stays in place regardless of collapsed/expanded state.
- On both breakpoints, the sidebar's width is capped at 60% of the modal's width — i.e. actual width is `min(300px, 60% of modal width)` — so the cap overrides the 300px default on narrow viewports.

This is a view-only feature: no restore/revert-to-version action, no copy-to-clipboard button. Just browsing.

## Out of Scope

- Restoring or reverting the file to a past version.
- Resizing or collapsing the sidebar on desktop.
- Pagination or lazy-loading of versions — the endpoint always returns the full list.
- Tier grouping or tier badges in the sidebar UI.
- A shared component between SearchSnippet's read-only rendering and HistoryModal's content viewer.
- History for non-text files — the FileTree already only lists text files, so this never comes up.
- User-configurable retention settings (already out of scope from the File Version Retention spec).
