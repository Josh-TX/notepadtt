## Glossary

See GLOSSARY.md for details. The app is a stateless server over a directory on disk (no DB); the design is recorded in rewrite.md.

**FileId**: in-memory id assigned to a file when it gets a tab; stable across renames; all edit/WS/tab logic uses it
**VersionId**: id of a file's current in-memory content; included in content reads/writes/broadcasts; drives concurrent-edit conflict detection
**RecentFileVersions**: In-memory cache of recent content snapshots per file; used for conflict resolution; entries purged after 5s
**Tab list**: Open files, in server memory, shared live across clients; empty at start; the active tab is per client
**Data root**: Directory served; `.git`, `node_modules`, `.ntt-trash` are invisible everywhere
**Trash**: `.ntt-trash` in the data root; deleted entries live there 5s for UNDO; wiped at startup
**Search**: Search Modal (Ctrl+Shift+F); server shells out to rg/grep; top 10 results. Ctrl+F is the browser's native find
**TabBar**: The only top bar: sidebar toggle, tabs (hover = absolute path, close always visible), "+" new file, search button
**Sidebar**: Side panel with just the FileTree; overlays on mobile
**FileTree**: Folders then files, case-insensitive alphabetical; watcher keeps it live
**Editor**: Full-viewport CodeMirror 5 text editor between TabBar and Footer; word wrap + line numbers only
**Footer**: Bottom bar with undo/redo/word-wrap toggles on left and file stats on right
**cid**: WS connection identifier; server assigns on connect, client includes as `?cid=` in REST requests
**Subscription**: Which file a client is currently subscribed to; established on file fetch, replaced on tab switch, reset on reconnect
