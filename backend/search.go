package backend

import (
	"regexp"
	"sort"
	"strings"
)

// Scoring weights for SearchFiles. Path matches count PathWeight times as much as
// Content matches (mirrors the old bm25 3:1 path weighting). Non-regex mode further
// multiplies both by 1 + TermMultiplierPerTerm*distinctTermCount, rewarding rows that
// match more of the distinct query terms. Source weights are applied last, on top of
// the path/content score, so a strong match in a lower-priority source can still
// outscore a weak match in a higher-priority one.
const (
	pathWeight = 3.0

	termMultiplierBase    = 1.0
	termMultiplierPerTerm = 0.5

	filesSourceWeight   = 5.0
	trashSourceWeight   = 2.0
	historySourceWeight = 1.0

	// minTermLength discards non-regex query terms shorter than this, mirroring the
	// effective floor the old FTS5 trigram index imposed on short queries. Regex mode
	// has no minimum.
	minTermLength = 2
)

// SearchOptions bundles the per-request source/mode toggles (IncludeHistory/
// IncludeTrash/Regex, controlled by SearchModal's live checkboxes) with the
// result-shaping knobs that come from Settings (LinesPerResult/MaxResultsPerFile/
// MaxFiles).
type SearchOptions struct {
	IncludeHistory    bool
	IncludeTrash      bool
	Regex             bool
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

// RegexCompileError wraps a regexp.Compile failure on a user-supplied pattern, letting
// callers (handleSearch) distinguish a bad pattern (400) from an internal DB error (500).
type RegexCompileError struct {
	err error
}

func (e *RegexCompileError) Error() string { return e.err.Error() }
func (e *RegexCompileError) Unwrap() error { return e.err }

// searchCandidate is one not-yet-scored, not-yet-sectioned match from a single source
// table, carrying just enough to compute a score and then build a SearchResult once
// the (more expensive) section extraction runs on the final, already-capped result set.
type searchCandidate struct {
	FileId    string
	Path      string
	Content   string
	VersionId string
	Source    string
}

// SearchFiles searches files/FileTrash/FileVersions for query, in either whitespace-
// split-terms LIKE mode (default) or single-pattern regex mode (opts.Regex). All
// matches across all three enabled sources are scored (see the weight constants above)
// and merged into one list sorted purely by score, capped to opts.MaxFiles. Returns
// (nil, nil) for an empty (or, in non-regex mode, all-too-short) query, and a
// *RegexCompileError if opts.Regex is set and query fails to compile.
func (d *DB) SearchFiles(query string, opts SearchOptions) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if opts.Regex {
		return d.searchFilesRegex(query, opts)
	}
	return d.searchFilesLike(query, opts)
}

// searchFilesLike implements non-regex mode: terms are OR'd across Path/Content via
// SQL LIKE narrowing (rather than pulling every row into Go and filtering with
// strings.Contains), then scored in Go once only the matching rows come back.
func (d *DB) searchFilesLike(query string, opts SearchOptions) ([]SearchResult, error) {
	terms := significantTerms(strings.Fields(query))
	if len(terms) == 0 {
		return nil, nil
	}
	where, args := buildLikeWhere(terms)
	scoreFn := func(c searchCandidate) float64 { return scoreLike(c.Path, c.Content, terms) }
	sectionsFn := func(content string) []SearchSection {
		return extractSections(content, terms, opts.LinesPerResult, opts.MaxResultsPerFile)
	}

	fileCandidates, err := d.fetchFileCandidates(where, args)
	if err != nil {
		return nil, err
	}
	claimed := claimedFileIds(fileCandidates)

	var trashCandidates []searchCandidate
	if opts.IncludeTrash {
		if trashCandidates, err = d.fetchTrashCandidates(where, args); err != nil {
			return nil, err
		}
		addClaimedFileIds(claimed, trashCandidates)
	}

	var historyCandidates []searchCandidate
	if opts.IncludeHistory {
		raw, err := d.fetchHistoryCandidates(where, args)
		if err != nil {
			return nil, err
		}
		historyCandidates = bestHistoryPerFileId(raw, claimed, scoreFn)
	}

	all := mergeCandidates(fileCandidates, trashCandidates, historyCandidates)
	return d.buildResults(all, opts, scoreFn, sectionsFn)
}

// searchFilesRegex implements regex mode: query is compiled once as a Go RE2 pattern
// and matched line-by-line against every candidate row pulled from the enabled source
// tables — there's no SQL-level narrowing (rejected; see spec), so every row from each
// enabled source is fetched and then filtered by pattern match in Go.
func (d *DB) searchFilesRegex(query string, opts SearchOptions) ([]SearchResult, error) {
	pattern, err := regexp.Compile(query)
	if err != nil {
		return nil, &RegexCompileError{err}
	}
	scoreFn := func(c searchCandidate) float64 { return scoreRegex(c.Path, c.Content, pattern) }
	sectionsFn := func(content string) []SearchSection {
		return extractSectionsRegex(content, pattern, opts.LinesPerResult, opts.MaxResultsPerFile)
	}

	fileCandidates, err := d.fetchFileCandidates("", nil)
	if err != nil {
		return nil, err
	}
	fileCandidates = filterRegexMatches(fileCandidates, pattern)
	claimed := claimedFileIds(fileCandidates)

	var trashCandidates []searchCandidate
	if opts.IncludeTrash {
		if trashCandidates, err = d.fetchTrashCandidates("", nil); err != nil {
			return nil, err
		}
		trashCandidates = filterRegexMatches(trashCandidates, pattern)
		addClaimedFileIds(claimed, trashCandidates)
	}

	var historyCandidates []searchCandidate
	if opts.IncludeHistory {
		raw, err := d.fetchHistoryCandidates("", nil)
		if err != nil {
			return nil, err
		}
		raw = filterRegexMatches(raw, pattern)
		historyCandidates = bestHistoryPerFileId(raw, claimed, scoreFn)
	}

	all := mergeCandidates(fileCandidates, trashCandidates, historyCandidates)
	return d.buildResults(all, opts, scoreFn, sectionsFn)
}

// significantTerms drops query terms shorter than minTermLength.
func significantTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if len(t) >= minTermLength {
			out = append(out, t)
		}
	}
	return out
}

// buildLikeWhere builds a SQL WHERE fragment OR-ing every term across Path/Content,
// with placeholders for each term's escaped, wildcard-wrapped LIKE pattern.
func buildLikeWhere(terms []string) (string, []any) {
	parts := make([]string, len(terms))
	args := make([]any, 0, len(terms)*2)
	for i, term := range terms {
		pattern := "%" + escapeLikeTerm(term) + "%"
		parts[i] = `(Content LIKE ? ESCAPE '\' OR Path LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}
	return strings.Join(parts, " OR "), args
}

// escapeLikeTerm escapes LIKE wildcard characters (% and _), plus the escape
// character itself, so a literal % or _ in a user-supplied term matches literally
// rather than acting as a wildcard.
func escapeLikeTerm(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(term)
}

// fetchFileCandidates queries the live files table, optionally narrowed by a LIKE
// where+args pair (empty where fetches every row, used by regex mode).
func (d *DB) fetchFileCandidates(where string, args []any) ([]searchCandidate, error) {
	return d.fetchCandidates(`SELECT FileId, Path, Content FROM files`, where, args, "file")
}

// fetchTrashCandidates queries FileTrash the same way fetchFileCandidates queries files.
func (d *DB) fetchTrashCandidates(where string, args []any) ([]searchCandidate, error) {
	return d.fetchCandidates(`SELECT FileId, Path, Content FROM FileTrash`, where, args, "trash")
}

func (d *DB) fetchCandidates(baseQuery, where string, args []any, source string) ([]searchCandidate, error) {
	query := baseQuery
	if where != "" {
		query += ` WHERE ` + where
	}
	rows, err := d.sql.Query(query, args...)
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
		c.Source = source
		out = append(out, c)
	}
	return out, rows.Err()
}

// fetchHistoryCandidates queries FileVersions, the one source with an extra VersionId
// column and (potentially) many rows per FileId — dedup to one-per-FileId happens
// afterward in bestHistoryPerFileId, not here.
func (d *DB) fetchHistoryCandidates(where string, args []any) ([]searchCandidate, error) {
	query := `SELECT FileId, Path, Content, VersionId FROM FileVersions`
	if where != "" {
		query += ` WHERE ` + where
	}
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.FileId, &c.Path, &c.Content, &c.VersionId); err != nil {
			continue
		}
		c.Source = "history"
		out = append(out, c)
	}
	return out, rows.Err()
}

// filterRegexMatches keeps only candidates where pattern matches Path or at least one
// line of Content — needed in regex mode since there's no SQL-level narrowing, so every
// fetched row must be checked before it's treated as a match.
func filterRegexMatches(candidates []searchCandidate, pattern *regexp.Regexp) []searchCandidate {
	var out []searchCandidate
	for _, c := range candidates {
		if pattern.MatchString(c.Path) || matchesAnyLine(c.Content, pattern) {
			out = append(out, c)
		}
	}
	return out
}

func matchesAnyLine(content string, pattern *regexp.Regexp) bool {
	for _, line := range strings.Split(content, "\n") {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// claimedFileIds/addClaimedFileIds track which FileIds already have a result from a
// higher-priority source (Files, then Trash), so History never produces a second
// result for a FileId that's already represented.
func claimedFileIds(candidates []searchCandidate) map[string]bool {
	m := make(map[string]bool, len(candidates))
	addClaimedFileIds(m, candidates)
	return m
}

func addClaimedFileIds(m map[string]bool, candidates []searchCandidate) {
	for _, c := range candidates {
		m[c.FileId] = true
	}
}

// bestHistoryPerFileId collapses possibly-many matching FileVersions rows per FileId
// down to the single best-scoring one (a file can have many historical snapshots, but
// only its best match should ever surface), excluding any FileId already claimed by a
// higher-priority source.
func bestHistoryPerFileId(raw []searchCandidate, claimed map[string]bool, scoreFn func(searchCandidate) float64) []searchCandidate {
	bestScore := map[string]float64{}
	best := map[string]searchCandidate{}
	for _, c := range raw {
		if claimed[c.FileId] {
			continue
		}
		s := scoreFn(c)
		if prev, ok := bestScore[c.FileId]; !ok || s > prev {
			bestScore[c.FileId] = s
			best[c.FileId] = c
		}
	}
	out := make([]searchCandidate, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	return out
}

func mergeCandidates(groups ...[]searchCandidate) []searchCandidate {
	var all []searchCandidate
	for _, g := range groups {
		all = append(all, g...)
	}
	return all
}

// scoreLike computes a non-regex candidate's score: occurrence counts (not just
// presence) of every term across Path and Content, multiplied by 1 +
// termMultiplierPerTerm*distinctTermCount (distinctTermCount computed once across
// path+content combined), then path occurrences weighted pathWeight over content.
func scoreLike(path, content string, terms []string) float64 {
	lowerPath := strings.ToLower(path)
	lowerContent := strings.ToLower(content)
	var pathOcc, contentOcc, distinctTermCount int
	for _, term := range terms {
		lt := strings.ToLower(term)
		p := strings.Count(lowerPath, lt)
		c := strings.Count(lowerContent, lt)
		pathOcc += p
		contentOcc += c
		if p+c > 0 {
			distinctTermCount++
		}
	}
	multiplier := termMultiplierBase + termMultiplierPerTerm*float64(distinctTermCount)
	pathScore := float64(pathOcc) * multiplier * pathWeight
	contentScore := float64(contentOcc) * multiplier
	return pathScore + contentScore
}

// scoreRegex computes a regex candidate's score: raw weighted occurrence counts with
// no multiplier concept (only ever one pattern). Content is matched strictly per-line
// (no cross-line matching), so a line with N matches contributes N to the count.
func scoreRegex(path, content string, pattern *regexp.Regexp) float64 {
	pathOcc := len(pattern.FindAllStringIndex(path, -1))
	var contentOcc int
	for _, line := range strings.Split(content, "\n") {
		contentOcc += len(pattern.FindAllStringIndex(line, -1))
	}
	pathScore := float64(pathOcc) * pathWeight
	contentScore := float64(contentOcc)
	return pathScore + contentScore
}

func sourceWeight(source string) float64 {
	switch source {
	case "file":
		return filesSourceWeight
	case "trash":
		return trashSourceWeight
	case "history":
		return historySourceWeight
	}
	return 1
}

// buildResults scores every candidate (path/content score from scoreFn, times its
// source weight), sorts purely by that final score descending, caps to opts.MaxFiles,
// and only then runs the (more expensive) section extraction on the surviving set.
func (d *DB) buildResults(all []searchCandidate, opts SearchOptions, scoreFn func(searchCandidate) float64, sectionsFn func(string) []SearchSection) ([]SearchResult, error) {
	type scored struct {
		candidate searchCandidate
		score     float64
	}
	scoredList := make([]scored, 0, len(all))
	for _, c := range all {
		scoredList = append(scoredList, scored{c, scoreFn(c) * sourceWeight(c.Source)})
	}
	sort.SliceStable(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	if len(scoredList) > opts.MaxFiles {
		scoredList = scoredList[:opts.MaxFiles]
	}

	results := make([]SearchResult, 0, len(scoredList))
	for _, sc := range scoredList {
		c := sc.candidate
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
			Sections:  sectionsFn(c.Content),
		})
	}
	return results, nil
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
	lowerTerms := make([]string, 0, len(terms))
	for _, t := range terms {
		if lt := strings.ToLower(t); lt != "" {
			lowerTerms = append(lowerTerms, lt)
		}
	}
	return extractSectionsWithCounter(content, linesPerResult, maxResults, func(line string) int {
		return countMatches(line, lowerTerms)
	})
}

// extractSectionsRegex is extractSections' regex-mode counterpart: a line's match
// count is however many non-overlapping times pattern matches within it, rather than
// a count of distinct query terms present.
func extractSectionsRegex(content string, pattern *regexp.Regexp, linesPerResult, maxResults int) []SearchSection {
	return extractSectionsWithCounter(content, linesPerResult, maxResults, func(line string) int {
		return len(pattern.FindAllStringIndex(line, -1))
	})
}

// extractSectionsWithCounter holds the windowing/merging algorithm shared by
// extractSections and extractSectionsRegex; countFn is the only thing that differs
// between literal-term and regex matching.
func extractSectionsWithCounter(content string, linesPerResult, maxResults int, countFn func(string) int) []SearchSection {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return []SearchSection{{Snippet: content, StartLineNumber: 1}}
	}

	type scoredLine struct{ idx, count int }
	var scored []scoredLine
	for i, line := range lines {
		count := countFn(line)
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
