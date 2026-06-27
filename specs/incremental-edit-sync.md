# Incremental Edit Sync

## Description

I want to stop sending the entire file content on every keystroke. Today the Editor's CodeMirror `change` event triggers an HTTP `PATCH /api/files/:id` carrying the full file content plus `currentVersionId`/`newVersionId`. I want to replace this with a WebSocket-based protocol that sends only the change itself, and resolves conflicts by relocating the change against whatever the latest content turns out to be, rather than blindly rejecting it.

### Transport: WebSocket instead of HTTP PATCH

`PATCH /api/files/:id` (`handleWriteFile` in `backend/api.go`) is removed entirely. All content writes now go through a new WebSocket message instead. Initial file load (`GET /api/files/:id`, which also establishes the Subscription) is unchanged.

The backend's `ws.go` `readPump` is currently a no-op (client→server WS messages aren't processed at all today). It needs to parse incoming JSON messages, switch on a `type` discriminator, and dispatch `edit` messages to the new write-handling logic described below. The server already knows which cid/connection sent the message from the read loop itself — no need to pass cid in the message body (unlike the old PATCH's `?cid=` query param).

### Edit Message (client → server)

Sent once per CodeMirror `change` event — no batching, no debounce. The Editor's `cm.on('change', ...)` handler sends a WS message immediately for every change event, exactly mirroring today's "no debounce, one write per change event" behavior, just over WS instead of HTTP.

The payload mirrors CodeMirror 5's native change event shape directly — no diffing library needed on the frontend, since CodeMirror already knows exactly what changed:

```json
{
  "type": "edit",
  "fileId": "...",
  "currentVersionId": "...",
  "newVersionId": "...",
  "from": { "line": 0, "ch": 0 },
  "to": { "line": 0, "ch": 0 },
  "text": ["..."],
  "removed": ["..."]
}
```

`from`/`to` is the range being replaced (in the content as of `currentVersionId`), `text` is the array of new lines replacing it, `removed` is the array of lines that were there before. Versioning works the same as today: client reads `currentVersionId` from the store, generates a fresh `newVersionId`, optimistically sets `store.fileVersions[fileId] = newVersionId` before sending. The client does **not** wait for any acknowledgment — it assumes `newVersionId` is now current and keeps sending further chained edits off of it, until a WS message tells it otherwise (a `content` or `editConflict` message). Since a single WebSocket connection delivers messages in order, this chaining is safe — the server processes a given client's edits in the order they were sent.

### No Ack on Success

If the server applies an edit cleanly, it sends nothing back to the sender — silence means success, since the client already has the edit applied locally. The only messages a sender receives are `content` broadcasts (when something changes that it doesn't already know about) and `editConflict` (when its own edit was rejected).

### Happy Path (versions match)

If `currentVersionId` equals the DB's latest `VersionId`, apply `from`/`to`/`text`/`removed` directly to the latest content to produce the new content. This is unchanged from today's structure otherwise:
1. Update the `files` table with the new content and `newVersionId` (TOCTOU-safe conditional update, same as today's `UpdateContentAndVersionIf`)
2. Write to disk
3. Store the new content in RecentFileVersions under `newVersionId`
4. Broadcast `{ type: "content", fileId, content, versionId: newVersionId }` to all other subscribed clients (excluding the sender)

If the conditional update fails because another write raced in between, treat it as a conflict (see below) rather than erroring.

### Conflict Path (versions don't match)

Look up `currentVersionId` in RecentFileVersions for that file.

**No matching snapshot found:** true conflict. Send `{ type: "editConflict", fileId, content: <latest DB content>, versionId: <latest DB versionId> }` directly to the sender only (not broadcast). Make no DB changes.

**Matching snapshot found:** this is the old content the client was editing from.
1. Validate that `removed` actually matches the snapshot's content at `from`/`to`. If it doesn't (desync), treat this exactly like "no matching snapshot" above — send `editConflict` to the sender, make no changes.
2. Otherwise, attempt relocation: line-diff the snapshot against the latest DB content using `diffmatchpatch`'s line-mode diffing (already a backend dependency) to build an old-line→new-line mapping.
3. If every line spanned by `from.line`..`to.line` (in the snapshot's line numbering) falls within an unmodified/equal hunk of that diff, relocation succeeds: map `from`/`to` to their corresponding line numbers in the latest content (character offsets within those lines are unchanged, since the line text itself didn't change), then apply `removed`/`text` at the relocated position against the latest content to produce the merged content. This is what handles cases like "a line got inserted earlier in the file, shifting line 10 to line 11" — the edit still lands in the right place.
4. If any line in that range falls within a changed (inserted/deleted/modified) hunk, that's a true, unresolvable conflict — same as "no matching snapshot": send `editConflict` to the sender only, make no changes.

**Relocation succeeded:**
1. Generate a **fresh** server-minted VersionId for the merged content (do not reuse the client's own `newVersionId` — the merged content differs from what the client computed locally, since it also incorporates other clients' edits the client never saw).
2. Commit the merged content + fresh VersionId to the DB (TOCTOU-safe conditional update against whatever VersionId was read as "latest"; if it raced, re-read and retry the relocation against the newer content).
3. Write to disk.
4. Store the merged content in RecentFileVersions under the fresh VersionId.
5. **Also** store an entry in RecentFileVersions under the client's own submitted `newVersionId`, containing the *naive* (non-merged) content — i.e. the snapshot with just this client's `removed`/`text` applied, with no relocation and no incorporation of other clients' edits. This is what the client's own local editor actually contains. Storing it lets the client's *next* chained edit (which will arrive with `currentVersionId` set to this `newVersionId`) still find a snapshot to diff against and go through the same relocate-or-conflict logic, even though this exact VersionId was never written to the DB.
6. Broadcast `{ type: "content", fileId, content: <merged content>, versionId: <fresh VersionId> }` to **all** subscribed clients, including the original sender — the sender's local content is missing the other clients' edits that got folded into the merge, so it needs the update too.

For a true conflict (no snapshot, validation mismatch, or overlapping hunks), nothing is stored under the client's `newVersionId` — the edit is simply dropped, consistent with "the existing change wins." If the client had already fired further chained edits before receiving the `editConflict` notice, those will also fail to find a snapshot once they arrive and will likewise bounce with their own `editConflict` messages. This cascading is accepted as-is (see Out of Scope).

### EditConflict Message (server → sender only)

```json
{ "type": "editConflict", "fileId": "...", "content": "...", "versionId": "..." }
```

Sent only to the client whose edit was rejected, never broadcast. On receipt, the frontend shows the same "your change was overridden by another client" toast that today's HTTP 409 path shows, and overwrites `store.fileContents[fileId]` / `store.fileVersions[fileId]` with the message's content/versionId. The Editor reactively picks up the content change, same as it does today for 409s and for ordinary `content` broadcasts.

### Existing `content` Broadcast Handling Is Unchanged

Whether a `content` message arrives because another client's edit landed cleanly, or because this client's own edit triggered a resolved merge, the client treats it identically to how it treats `content` broadcasts today: overwrite local content/version, no toast. If the sender had typed further keystrokes locally between sending the edit and receiving the broadcast, those may get clobbered by the overwrite — this is an accepted limitation (see Out of Scope), not new behavior introduced by this change (today's `content` broadcast already does a blind overwrite).

### Frontend Changes

- `frontend/src/api.js`'s HTTP-based `writeFile` (and its 409-handling branch) is removed.
- `frontend/src/components/Editor.vue`'s `cm.on('change', ...)` handler sends the WS `edit` message described above instead of calling `writeFile`. The existing `ignoreNextChange` guard (used to suppress sending when the editor's content was just set programmatically, e.g. from a remote update) continues to apply to this new send path the same way it gated the old `writeFile` call.
- `frontend/src/store.js`'s WS `onmessage` handler gains a case for `type === 'editConflict'`: update `state.fileContents`/`state.fileVersions` and trigger the conflict toast (reusing whatever toast mechanism `api.js` used for the old 409 path).
- The version-tracking logic (read `currentVersionId`, generate `newVersionId` via the existing JS `uniqueId(5)`-equivalent, optimistically set `store.fileVersions[fileId]`) moves to wherever the new WS send happens, but otherwise works exactly as it does today.

## Out of Scope

- Smarter client-side reconciliation when a client's own not-yet-acknowledged local keystrokes collide with an incoming `content` broadcast (for now, blind overwrite is fine — explicitly deferred for future work).
- Deduplicating or suppressing multiple `editConflict` toasts if a client's chained in-flight edits cascade-fail one after another following an initial true conflict.
- Any change to RecentFileVersions' purge timing (5s expiry / 1-minute sweep) or its underlying data structure.
- Any change to the initial file load (`GET /api/files/:id`) or Subscription establishment flow.
- Batching or debouncing WS edit messages — explicitly one message per CodeMirror change event.
