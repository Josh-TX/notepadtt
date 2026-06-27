## Glossary

**FileId**: uniqueId string assigned to each file; stable across renames; all frontend/backend logic uses this
**VersionId**: File VersionId; stored in DB; included in all content reads/writes/broadcasts; drives concurrent-edit conflict detection
**RecentFileVersions**: In-memory cache of recent content snapshots per file; used for conflict resolution; entries purged after 5s
**FileVersion**: persisted past-content snapshot per file; tagged with retention Term + supersede Date
**Term**: FileVersion's retention tier; Tiers are cumulative
**FileVersioning process**: Background Job that manages FileVersions and various TTL expirys
**FileTrash**: Holds deleted files for restore; auto-purged after TrashTTL
**CurrentFolder**: Active folder shown in navbar; determines which tabs are visible and where new files are created
**TabBar**: Tab strip below navbar showing all text files in CurrentFolder; horizontally scrollable
**Search**: Workspace-wide file search triggered by the Search Modal. Uses FTS5 trigram and gives the top 10 highest ranked results
**Sidebar**: Side panel showing Search/Trash buttons + FileTree; overlays on mobile
**Navbar**: Topmost bar; sidebar toggle on left, "NTT" + breadcrumbs in middle, "+" new-file button on right; desktop crumbs clickable except last
**Editor**: Full-viewport CodeMirror 5 text editor between TabBar and Footer; word wrap + line numbers only
**Footer**: Bottom bar with undo/redo/word-wrap toggles on left and file stats (length, lines) on right
**HistoryModal**: Modal showing past FileVersions of a file; the modal's sidebar lists versions, right pane shows read-only content
**TrashModal**: Browse/restore trashed files; sidebar+filter on left, read-only content + restore on right
**FileTree**: Can mean either the FileTree in the sidebar, or the data structure containing all nested files; files ordered by OrderNum, folders alphabetical
**OrderNum**: 0-based integer per file; determines file order within a folder in TabBar and FileTree
**cid**: WS connection identifier; server assigns on connect, client includes as `?cid=` in REST requests
**Subscription**: Which file a client is currently subscribed to; established on file fetch, replaced on tab switch, reset on reconnect
