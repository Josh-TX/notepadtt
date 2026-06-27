# FTS5 Search

## Description

I want to add workspace-wide file search powered by SQLite FTS5 with a trigram tokenizer. The search covers path and content only.

### Database

The existing `files` SQLite table should be replaced entirely with a single FTS5 virtual table of the same name. There is no separate regular table and no triggers — the FTS5 table IS the files table. The schema looks like:

```sql
CREATE VIRTUAL TABLE files USING fts5(
  FileId UNINDEXED,
  Path,
  Content,
  LastOpened UNINDEXED,
  ContentUpdated UNINDEXED,
  OrderNum UNINDEXED,
  VersionId UNINDEXED,
  tokenize='trigram'
);
```

`Path` and `Content` are indexed by FTS5; all other columns are `UNINDEXED`. No migration is needed — the database will be deleted and recreated.

### Backend API

Add `GET /api/search?q=<query>`. The handler should:

1. Split the query string on whitespace to get individual terms.
2. Build an FTS5 MATCH expression joining all terms with OR (e.g. `term1 OR term2`), searching path and content columns.
3. Order results by `bm25(files, 3.0, 1.0)` — path weighted 3× over content.
4. Take the top 10 results.
5. For each matched file, extract a SearchSnippet: find the line whose content has the most case-insensitive substring matches against the search terms (first occurrence wins ties), then take 1 line before it + that line + 2 lines after it (fewer lines returned if at file boundaries).
6. Return a JSON array of up to 10 objects, each with: `fileId`, `path`, `snippet` (the 4-line excerpt as a string), `startLineNumber` (1-based line number of the first line in the snippet).

### Sidebar

The Sidebar gets a new Search section above the FileTree. It is a clickable row, nearly full-width, styled similarly to a FileTree row but slightly larger. It contains a magnifying glass icon on the left followed by the text "Search". Clicking it opens the SearchModal. Leave space conceptually on the right for a future settings button (not implemented now) — so do not crowd the right edge.

### SearchModal

The SearchModal is a full-screen overlay on mobile and a large centered modal with a max-width on desktop.

**Header (mobile):** a single row containing a text input (fills remaining width) and a close button on the right.  
**Header (desktop):** same layout, constrained by the modal's max-width.

The text input is autofocused when the modal opens. There is a 400ms debounce: any keystroke immediately clears current results and starts the debounce timer. A subtle loading indicator is shown during the debounce window and while the API call is in flight. When the query is empty, show the placeholder text "Type to search…" in the results area.

Results are displayed in one large scrollable section. Each result shows:
- The full relative file path at the top.
- The SearchSnippet below it: a read-only code block styled like the Editor (dark background #1f1f1f, monospace font) with actual file line numbers on the left. Matching search terms are highlighted in yellow/orange within the snippet text.

Each result is clickable/tappable. Clicking a result:
1. Closes the modal.
2. If the file is in a different folder than CurrentFolder, navigates to the file's folder (updates CurrentFolder).
3. Opens the file's tab.
4. Scrolls the editor to the matching line (even if the file was already the active file).

The SearchModal retains its last query and results in memory — reopening the modal shows exactly what was there when it was last closed. Results do not auto-refresh when files change on disk.

## Out of Scope

- Settings button alongside the Search row in the Sidebar (planned for later).
- Auto-refreshing search results on filesystem changes.
- Database migration — the existing DB will be deleted.
