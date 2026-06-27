# Minimal Edit Apply

## Description

CodeMirror 5 has a delay between when the user types and when that keystroke is reflected in the editor's internal Doc (`cm.getValue()`). Today, when a remote content update arrives (a WS `content` or `editConflict` message), `Editor.vue` calls `cm.setValue(content)` to apply it. `setValue` fully resets the Doc/input state, which destroys CodeMirror's own in-flight handling of the user's just-typed (but not-yet-polled) keystroke — even capturing `cm.getValue()` immediately before the `setValue` call doesn't help, since that keystroke isn't in the Doc yet either. The practical symptom: a user typing at the exact moment a remote update lands can see their character silently reverted.

The fix is to stop using `cm.setValue` for remote content updates and instead apply only the precise sub-range that actually changed, via `cm.replaceRange`. `replaceRange` doesn't reset the Doc/input state the way `setValue` does, so an in-flight keystroke survives and gets correctly applied on CodeMirror's next input poll.

This only changes the remote-update path. It does not touch the backend at all — the backend keeps broadcasting full file content (no diffs), and the tab-switch `setValue` call in `Editor.vue` (when `store.activeFileId` changes) is untouched, since there's no in-flight-edit risk when switching files (cursor/history are already reset there).

### Scope of the change

Only the content-listener callback in `Editor.vue` (currently around line 126-136, registered via `onContentUpdate(fileId, ...)`) is affected. This callback fires for both `content` broadcasts and `editConflict` messages (both flow through `applyContentUpdate` in `store.js`), so both are covered by this fix automatically since they share the same callback.

### Algorithm: smallest single edit via common prefix/suffix trim

Add a new pure function, `computeMinimalEdit(oldStr, newStr)`, in a new module `frontend/src/cmEdit.js`. It:

1. Returns `null` if `oldStr === newStr` (no-op).
2. Otherwise, finds the length of the longest common prefix between `oldStr` and `newStr`, and (independently, over the remaining non-prefix portion of each string) the longest common suffix.
3. Computes the single replaced range: the differing middle chunk of `oldStr` (between the prefix and suffix boundaries) gets replaced with the differing middle chunk of `newStr`.
4. Converts the prefix-length and prefix+middle-length character offsets (measured against `oldStr`) into CodeMirror `{line, ch}` positions.
5. Returns `{ from: {line, ch}, to: {line, ch}, text: <replacement string> }` describing exactly one `replaceRange`-compatible edit.

This always produces exactly one edit — even when there's no common prefix/suffix at all, it just degenerates to "replace the entire range," which is still a single edit. This single-edit property matters because `Editor.vue`'s existing `ignoreNextChange` flag only suppresses exactly one `change` event; a multi-hunk diff would require reworking that suppression mechanism, which is explicitly out of scope here.

`computeMinimalEdit` is a pure function with no CodeMirror dependency, so it can be unit tested in isolation against plain strings.

### Applying the edit in Editor.vue

In the `onContentUpdate` callback, replace the current:

```javascript
const cursor = cm.getCursor()
ignoreNextChange = true
cm.setValue(content)
cm.setCursor(cursor)
```

with logic that:

1. Calls `computeMinimalEdit(cm.getValue(), content)`. If it returns `null`, do nothing (already covered by the existing `cm.getValue() === content` early-return check).
2. Otherwise, sets `ignoreNextChange = true` and calls `cm.replaceRange(edit.text, edit.from, edit.to)` inside a try/catch.
3. Does **not** manually save/restore the cursor for this path — `replaceRange` keeps CodeMirror's live cursor/selection correctly positioned automatically (shifting it relative to the edit), so the existing manual `getCursor`/`setCursor` dance is unnecessary and is removed for this path.
4. If anything throws during step 2 (defensive safety net only — the algorithm itself always produces a valid edit, no deliberate skip conditions like file-size thresholds), fall back to the original behavior: `getCursor()` → `setValue(content)` → `setCursor(cursor)`, with `ignoreNextChange = true` set again before `setValue` (since the failed `replaceRange` attempt may or may not have already consumed the flag — set it again to be safe).

### Module shape

`frontend/src/cmEdit.js` exports only the pure diff function:

```javascript
export function computeMinimalEdit(oldStr, newStr) {
  // returns { from: {line, ch}, to: {line, ch}, text } or null
}
```

All CodeMirror-specific application logic (calling `replaceRange`, the try/catch, the `setValue` fallback, the `ignoreNextChange` flag) stays in `Editor.vue`. This keeps the diffing algorithm itself fully unit-testable without needing a CodeMirror instance.

## Out of Scope

- Any backend change. The server continues to broadcast full file content on every `content`/`editConflict` message; no diffing happens server-side.
- The tab-switch `setValue` call (triggered by `store.activeFileId` changing) — left exactly as-is.
- Multi-hunk/multi-edit diffing (e.g. line-based diff-match-patch like the backend uses for 3-way merge). The frontend only ever produces a single contiguous edit per update.
- Deliberate skip-conditions for the incremental path (e.g. file-size thresholds). The `setValue` fallback exists purely for defensive error-handling.
- Any change to scroll position handling, multi-cursor/selection support, or the search-highlight restoration logic already present in `Editor.vue`.
