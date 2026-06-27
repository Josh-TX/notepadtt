package backend

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// SearchOptions bundles the per-request source toggles (IncludeHistory/IncludeTrash,
// controlled by SearchModal's live checkboxes) with the result-shaping knobs that come
// from Settings (LinesPerResult/MaxResultsPerFile/MaxFiles).
type SearchOptions struct {
	IncludeHistory    bool
	IncludeTrash      bool
	LinesPerResult    int
	MaxResultsPerFile int
	MaxFiles          int
}

// SearchSection is one matched-and-windowed excerpt within a single file's content.
type SearchSection struct {
	Snippet         string `json:"snippet"`
	StartLineNumber int    `json:"startLineNumber"`
}

// SearchResult is the per-file result returned by GET /api/search. Source is "file",
// "history", or "trash". VersionId is only set for "history" results, identifying which
// FileVersion row to auto-select in HistoryModal. Exists is only meaningful for
// "history" results: whether the file is still live (Path is then its current path) or
// orphaned (Path falls back to that FileVersion row's stored historical path). "file"
// results are always Exists=true; "trash" results are always Exists=false.
type SearchResult struct {
	FileId    string          `json:"fileId"`
	Path      string          `json:"path"`
	Source    string          `json:"source"`
	VersionId string          `json:"versionId,omitempty"`
	Exists    bool            `json:"exists"`
	Sections  []SearchSection `json:"sections"`
}

// searchCandidate is one not-yet-sectioned match from a single source table, carrying
// just enough to build a SearchResult once the (more expensive) section extraction runs
// on the final, already-capped result set. Deliberately has no score field: bm25() is
// reliable when used in a query's own ORDER BY, but reading it out via SELECT into Go
// and then comparing/combining those values turned out not to be — see SearchFiles.
type searchCandidate struct {
	FileId    string
	Path      string
	Content   string
	VersionId string
	Source    string
}

// SearchFiles fills up to MaxFiles results by querying sources in strict priority
// order — Files, then Trash, then History — each one only queried while slots remain.
// There's no cross-source ranking: bm25() is reliable for ORDER BY but not as a value
// read out via SELECT, so results from different FTS5 tables are never compared against
// each other numerically. Instead each source is fully exhausted (up to its share of
// MaxFiles) before the next is even queried, and the final list is simply Files results
// followed by Trash results followed by History results.
func (d *DB) SearchFiles(terms []string, opts SearchOptions) ([]SearchResult, error) {
	if len(terms) == 0 {
		return nil, nil
	}

	matchExpr := buildMatchExpr(terms)

	fileCandidates, err := d.searchLiveFiles(matchExpr, opts.MaxFiles)
	if err != nil {
		return nil, err
	}
	remaining := opts.MaxFiles - len(fileCandidates)

	claimed := make(map[string]bool, len(fileCandidates))
	for _, c := range fileCandidates {
		claimed[c.FileId] = true
	}

	var trashCandidates []searchCandidate
	if opts.IncludeTrash && remaining > 0 {
		trashCandidates, err = d.searchTrash(matchExpr, remaining)
		if err != nil {
			return nil, err
		}
		for _, c := range trashCandidates {
			claimed[c.FileId] = true
		}
		remaining -= len(trashCandidates)
	}

	// History is the only source that can return many rows for the same FileId (a
	// file's past versions), so it can't just take a flat LIMIT like Files/Trash —
	// claimed gets one FileId added per loop iteration, and each iteration's query
	// excludes everything claimed so far, guaranteeing every row returned here is a
	// genuinely new FileId. See searchNextHistoryMatch.
	var historyCandidates []searchCandidate
	if opts.IncludeHistory {
		for remaining > 0 {
			c, found, err := d.searchNextHistoryMatch(matchExpr, claimed)
			if err != nil {
				return nil, err
			}
			if !found {
				break
			}
			claimed[c.FileId] = true
			historyCandidates = append(historyCandidates, c)
			remaining--
		}
	}

	all := make([]searchCandidate, 0, len(fileCandidates)+len(trashCandidates)+len(historyCandidates))
	all = append(all, fileCandidates...)
	all = append(all, trashCandidates...)
	all = append(all, historyCandidates...)

	results := make([]SearchResult, 0, len(all))
	for _, c := range all {
		path := c.Path
		exists := c.Source != "trash"
		if c.Source == "history" {
			f, err := d.GetFile(c.FileId)
			if err != nil {
				return nil, err
			}
			exists = f != nil
			if exists {
				path = f.Path
			}
		}
		results = append(results, SearchResult{
			FileId:    c.FileId,
			Path:      path,
			Source:    c.Source,
			VersionId: c.VersionId,
			Exists:    exists,
			Sections:  extractSections(c.Content, terms, opts.LinesPerResult, opts.MaxResultsPerFile),
		})
	}
	return results, nil
}

// buildMatchExpr builds an FTS5 MATCH expression OR-ing every term across the Path and
// Content columns, shared by all three source queries since files_fts, fileversions_fts,
// and filetrash_fts all index the same two column names.
func buildMatchExpr(terms []string) string {
	matchParts := make([]string, len(terms))
	for i, term := range terms {
		escaped := strings.ReplaceAll(term, `"`, `""`)
		matchParts[i] = fmt.Sprintf(`{path content}: "%s"`, escaped)
	}
	return strings.Join(matchParts, " OR ")
}

// searchLiveFiles queries files_fts for live-file matches, ranked by bm25 with Path
// weighted 3x over Content. One row per FileId is guaranteed by the files table itself,
// so no dedup is needed here.
func (d *DB) searchLiveFiles(matchExpr string, limit int) ([]searchCandidate, error) {
	rows, err := d.sql.Query(
		`SELECT f.FileId, f.Path, f.Content FROM files_fts
		 JOIN files f ON f.Id = files_fts.rowid
		 WHERE files_fts MATCH ?
		 ORDER BY bm25(files_fts, 3.0, 1.0) LIMIT ?`,
		matchExpr, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.FileId, &c.Path, &c.Content); err != nil {
			continue
		}
		c.Source = "file"
		out = append(out, c)
	}
	return out, rows.Err()
}

// searchTrash queries filetrash_fts for matching trashed files, ranked by bm25. At most
// one FileTrash row exists per FileId at a time (restore/purge always clears the prior
// row first), and a FileId can never have both a live files row and a FileTrash row
// (TrashFile deletes the former before inserting the latter) — so no dedup or
// exclusion against Files is needed here either.
func (d *DB) searchTrash(matchExpr string, limit int) ([]searchCandidate, error) {
	rows, err := d.sql.Query(
		`SELECT ft.FileId, ft.Path, ft.Content FROM filetrash_fts
		 JOIN FileTrash ft ON ft.Id = filetrash_fts.rowid
		 WHERE filetrash_fts MATCH ?
		 ORDER BY bm25(filetrash_fts, 3.0, 1.0) LIMIT ?`,
		matchExpr, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.FileId, &c.Path, &c.Content); err != nil {
			continue
		}
		c.Source = "trash"
		out = append(out, c)
	}
	return out, rows.Err()
}

// searchNextHistoryMatch returns the single best-scoring fileversions_fts match whose
// FileId isn't in excludeFileIds, or found=false once no such match remains. A single
// FileId can have many matching FileVersion rows (e.g. many near-duplicate edits), so
// rather than fetching a batch and picking the best per FileId by comparing scores
// (unreliable once read out of SQLite), the caller loops this one-row-at-a-time,
// growing excludeFileIds by the FileId returned each time — ORDER BY ... LIMIT 1
// reliably hands back the best remaining row, which is guaranteed to be a new FileId
// since every FileId seen so far is excluded from the query.
func (d *DB) searchNextHistoryMatch(matchExpr string, excludeFileIds map[string]bool) (searchCandidate, bool, error) {
	query := `SELECT fv.FileId, fv.Path, fv.Content, fv.VersionId FROM fileversions_fts
	          JOIN FileVersions fv ON fv.Id = fileversions_fts.rowid
	          WHERE fileversions_fts MATCH ?`
	args := []any{matchExpr}
	if len(excludeFileIds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(excludeFileIds)), ",")
		query += ` AND fv.FileId NOT IN (` + placeholders + `)`
		for id := range excludeFileIds {
			args = append(args, id)
		}
	}
	query += ` ORDER BY bm25(fileversions_fts, 3.0, 1.0) LIMIT 1`

	var c searchCandidate
	err := d.sql.QueryRow(query, args...).Scan(&c.FileId, &c.Path, &c.Content, &c.VersionId)
	if err == sql.ErrNoRows {
		return searchCandidate{}, false, nil
	}
	if err != nil {
		return searchCandidate{}, false, err
	}
	c.Source = "history"
	return c, true, nil
}

// extractSections finds up to maxResults match-line clusters in content and returns
// them as windowed snippets, ordered top-to-bottom by line number. Each candidate match
// line is the one with the most case-insensitive term matches (ties go to the earlier
// line, mirroring the original single-snippet behavior); its window is sized by
// linesPerResult lines before/after it. Overlapping or touching windows are merged into
// one continuous section, even if that makes it longer than linesPerResult — so the
// final section count can end up below maxResults, and no backfill is attempted to
// compensate. If no line actually matches (e.g. only the path matched), a single
// section anchored at line 1 is returned, matching the original fallback.
func extractSections(content string, terms []string, linesPerResult, maxResults int) []SearchSection {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return []SearchSection{{Snippet: content, StartLineNumber: 1}}
	}

	lowerTerms := make([]string, 0, len(terms))
	for _, t := range terms {
		if lt := strings.ToLower(t); lt != "" {
			lowerTerms = append(lowerTerms, lt)
		}
	}

	type scoredLine struct{ idx, count int }
	var scored []scoredLine
	for i, line := range lines {
		count := countMatches(line, lowerTerms)
		if count > 0 {
			scored = append(scored, scoredLine{i, count})
		}
	}
	if len(scored) == 0 {
		scored = []scoredLine{{0, 0}}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].count != scored[j].count {
			return scored[i].count > scored[j].count
		}
		return scored[i].idx < scored[j].idx
	})
	if len(scored) > maxResults {
		scored = scored[:maxResults]
	}

	before, after := windowSize(linesPerResult)
	type window struct{ start, end int }
	windows := make([]window, len(scored))
	for i, sl := range scored {
		start := sl.idx - before
		if start < 0 {
			start = 0
		}
		end := sl.idx + after
		if end >= len(lines) {
			end = len(lines) - 1
		}
		windows[i] = window{start, end}
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].start < windows[j].start })

	var merged []window
	for _, w := range windows {
		if len(merged) > 0 && w.start <= merged[len(merged)-1].end+1 {
			if w.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = w.end
			}
			continue
		}
		merged = append(merged, w)
	}

	sections := make([]SearchSection, len(merged))
	for i, w := range merged {
		sections[i] = SearchSection{
			Snippet:         strings.Join(lines[w.start:w.end+1], "\n"),
			StartLineNumber: w.start + 1,
		}
	}
	return sections
}

// windowSize returns how many lines of context to take before/after a matched line,
// given linesPerResult total. With 3+ lines there's room for 1 before plus the rest
// after (generalizing the original hardcoded "1 before + match + 2 after" = 4 total);
// with 1-2 lines the match line itself takes priority, so there's nothing before it.
func windowSize(linesPerResult int) (before, after int) {
	if linesPerResult >= 3 {
		return 1, linesPerResult - 2
	}
	return 0, linesPerResult - 1
}

// countMatches returns how many of the (already-lowercased) terms appear as a
// case-insensitive substring of line.
func countMatches(line string, lowerTerms []string) int {
	lower := strings.ToLower(line)
	count := 0
	for _, t := range lowerTerms {
		if strings.Contains(lower, t) {
			count++
		}
	}
	return count
}
