# Sqlite Regex Search

## Description

I want to remove SQLite FTS5 from search entirely (replacing the FTS5 Search and Search Extended Sources specs) and query the existing normal tables (`files`, `FileVersions`, `FileTrash`) directly instead. FTS5 is overkill for the actual usecase — a relatively small amount of text per workspace — and it doesn't support regex, which I want. This means dropping all FTS5 virtual tables (`files_fts`, `fileversions_fts`, `filetrash_fts`), their shadow tables, the debounced sync logic in `ftssync.go`, and the `fileversions`/`filetrash` triggers. There is no production database to migrate, so the old FTS5 schema and data can just be removed with no migration path needed.

Add a "regex" checkbox to the SearchModal, matching the existing in-memory-only persistence behavior of the History/Trash checkboxes: it persists across modal open/close within a session but resets on page refresh (no localStorage). It maps to a boolean query param on the search request.

### Non-regex mode (checkbox off, default)
Behavior matches today: the query splits on whitespace into terms, OR'd together across Path and Content. Matching is done via SQL `LIKE` narrowing — `WHERE Content LIKE '%term%' OR ... OR Path LIKE '%term%' OR ...` — rather than pulling every row into Go and filtering with `strings.Contains`. This was benchmarked against the real `frontend/node_modules` corpus (1,910 files) across 1-10 OR'd terms: `LIKE` narrowing won every time, from ~3.2x faster at 1 term down to ~2.6x faster at 10 terms, because SQLite filters internally and only matching rows' content ever gets marshalled into Go. Literal `%` and `_` characters within user-supplied terms must be escaped before building the `LIKE` pattern so they match literally instead of acting as wildcards. The existing 2-character minimum query length still applies in this mode.

### Regex mode (checkbox on)
The raw query text is used directly as a Go `regexp` (RE2) pattern — no whitespace splitting. Matching is done via a plain Go loop: pull all candidate rows into memory and run `regexp.MatchString` per row. This was benchmarked against registering a custom SQLite `REGEXP` scalar function (via `modernc.org/sqlite`'s `RegisterDeterministicScalarFunction`) and found to be equal or very slightly faster than the UDF approach — since the driver is pure Go with no C-level fast path, there's no real benefit to pushing the match into SQL, so the UDF idea is rejected as unneeded complexity.

Regex matching is strictly per-line: file content is split into lines and the pattern is tested against each line independently. There is no cross-line/multi-line matching — `^`/`$` implicitly behave as per-line anchors (equivalent to always applying `(?m)` per individual line), achieved via literal line-splitting rather than a multiline regex flag. If a single line has multiple matches, each counts separately toward the occurrence count. There is no minimum query length in regex mode. If the pattern fails to compile, show an inline error near the search box/checkbox and don't run the search.

### Scoring (replaces bm25)
Ranking is done via a manually computed score using named constants (no magic numbers) for the weights below.

For each matched file/version/trash row, compute:
- `sumPathOccurrences` / `sumContentOccurrences`: total occurrence counts (not just presence) — e.g. a term/pattern appearing 3 times counts as 3, summed across all matching terms/lines.
- Non-regex mode only: `distinctTermCount` = number of distinct query terms found anywhere across path+content combined (computed once globally, not separately per column). Multiplier = `1 + 0.5 * distinctTermCount`, applied to both path and content occurrence sums.
  - Regex mode has no multiplier concept (only ever one pattern) — it uses raw weighted occurrence counts directly.
- `pathScore = sumPathOccurrences * multiplier * PathWeight` where `PathWeight = 3` (matches today's bm25 3:1 path weighting).
- `contentScore = sumContentOccurrences * multiplier * 1`.
- `totalScore = pathScore + contentScore`.
- Source weighting is then applied on top, via named constants: Files ×5, Trash ×2, History ×1.

All matches across all three sources (files, trash, history) are merged into one list and sorted purely by final weighted score — this replaces the current strict Files-then-Trash-then-History priority ordering.

History still needs one result per FileId (its best-scoring matching `FileVersions` row), since a file can have many historical snapshots — same dedup behavior as today. Trash does not need this dedup since at most one live `FileTrash` row exists per FileId at a time.

Result count cap and snippet/context extraction (`MaxFiles`, `LinesPerResult`, `MaxResultsPerFile` settings) are unchanged from the current implementation.

## Out of Scope
- No SQLite `REGEXP` UDF — benchmarked and rejected, no meaningful perf gain over the Go-loop approach with this pure-Go driver.
- No DB schema migration for old FTS5 tables/data — no production database exists to migrate.
- No literal-substring prefiltering optimization for regex mode (e.g. extracting a literal prefix from the pattern to narrow via `LIKE` first) — full Go-loop scan is acceptable given expected workspace sizes.
- No configurable path/content/source weight constants via Settings — these remain hardcoded (named) constants.
- No changes to snippet/context display settings or behavior.
