package backend

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractSections(t *testing.T) {
	t.Run("single match returns one section sized by linesPerResult", func(t *testing.T) {
		content := "alpha\nbeta gamma\ndelta\nepsilon"
		sections := extractSections(content, []string{"beta", "gamma"}, 4, 2)
		if len(sections) != 1 {
			t.Fatalf("expected 1 section, got %+v", sections)
		}
		if sections[0].StartLineNumber != 1 {
			t.Fatalf("expected startLineNumber=1, got %d", sections[0].StartLineNumber)
		}
		if !strings.Contains(sections[0].Snippet, "alpha") || !strings.Contains(sections[0].Snippet, "beta gamma") || !strings.Contains(sections[0].Snippet, "delta") {
			t.Fatalf("unexpected snippet: %q", sections[0].Snippet)
		}
	})

	t.Run("clamps window at start of file", func(t *testing.T) {
		content := "match here\nline2\nline3\nline4"
		sections := extractSections(content, []string{"match"}, 4, 2)
		if len(sections) != 1 || sections[0].StartLineNumber != 1 {
			t.Fatalf("expected 1 section starting at line 1, got %+v", sections)
		}
		if strings.Split(sections[0].Snippet, "\n")[0] != "match here" {
			t.Fatalf("expected first snippet line to be 'match here', got %q", sections[0].Snippet)
		}
	})

	t.Run("clamps window at end of file", func(t *testing.T) {
		content := "line1\nline2\nmatch here"
		sections := extractSections(content, []string{"match"}, 4, 2)
		if len(sections) != 1 || sections[0].StartLineNumber != 2 {
			t.Fatalf("expected 1 section starting at line 2, got %+v", sections)
		}
		if !strings.Contains(sections[0].Snippet, "line2") || !strings.Contains(sections[0].Snippet, "match here") {
			t.Fatalf("unexpected snippet: %q", sections[0].Snippet)
		}
	})

	t.Run("picks first occurrence on tie", func(t *testing.T) {
		content := "foo line\nbar line\nbaz"
		sections := extractSections(content, []string{"foo", "bar"}, 4, 1)
		if len(sections) != 1 || sections[0].StartLineNumber != 1 {
			t.Fatalf("expected first-occurrence line to win the tie, got %+v", sections)
		}
	})

	t.Run("single line file returns that line", func(t *testing.T) {
		sections := extractSections("only line", []string{"only"}, 4, 2)
		if len(sections) != 1 || sections[0].StartLineNumber != 1 || sections[0].Snippet != "only line" {
			t.Fatalf("got %+v", sections)
		}
	})

	t.Run("path-only match with no in-content hits anchors at line 1", func(t *testing.T) {
		sections := extractSections("nothing relevant\nhere either", []string{"zzz"}, 4, 2)
		if len(sections) != 1 || sections[0].StartLineNumber != 1 {
			t.Fatalf("expected fallback anchor at line 1, got %+v", sections)
		}
	})

	t.Run("distant matches produce multiple ordered sections capped at maxResults", func(t *testing.T) {
		// 20 lines, matches at index 1 and 15 (far apart), plus a third weaker match.
		lines := make([]string, 20)
		for i := range lines {
			lines[i] = "filler"
		}
		lines[1] = "needle one"
		lines[15] = "needle two"
		content := strings.Join(lines, "\n")

		sections := extractSections(content, []string{"needle"}, 4, 2)
		if len(sections) != 2 {
			t.Fatalf("expected 2 sections capped by maxResults=2, got %+v", sections)
		}
		if sections[0].StartLineNumber >= sections[1].StartLineNumber {
			t.Fatalf("expected sections ordered top-to-bottom by line number, got %+v", sections)
		}
		if !strings.Contains(sections[0].Snippet, "needle one") {
			t.Fatalf("expected first section to contain the earlier match, got %q", sections[0].Snippet)
		}
		if !strings.Contains(sections[1].Snippet, "needle two") {
			t.Fatalf("expected second section to contain the later match, got %q", sections[1].Snippet)
		}
	})

	t.Run("overlapping windows merge into one continuous section exceeding linesPerResult", func(t *testing.T) {
		// linesPerResult=4 -> window is 1 before + match + 2 after (span 4). Matches at
		// index 2 and 4 are close enough that their windows overlap.
		content := strings.Join([]string{"l0", "l1", "needle a", "l3", "needle b", "l5", "l6"}, "\n")
		sections := extractSections(content, []string{"needle"}, 4, 2)
		if len(sections) != 1 {
			t.Fatalf("expected windows to merge into 1 section, got %+v", sections)
		}
		lineCount := len(strings.Split(sections[0].Snippet, "\n"))
		if lineCount <= 4 {
			t.Fatalf("expected merged section to exceed linesPerResult=4, got %d lines: %q", lineCount, sections[0].Snippet)
		}
		if !strings.Contains(sections[0].Snippet, "needle a") || !strings.Contains(sections[0].Snippet, "needle b") {
			t.Fatalf("expected merged section to contain both matches, got %q", sections[0].Snippet)
		}
	})

	t.Run("linesPerResult<=2 puts the match line first with nothing before it", func(t *testing.T) {
		content := strings.Join([]string{"before", "match here", "after1", "after2"}, "\n")
		sections := extractSections(content, []string{"match"}, 2, 1)
		if len(sections) != 1 {
			t.Fatalf("expected 1 section, got %+v", sections)
		}
		got := strings.Split(sections[0].Snippet, "\n")
		if got[0] != "match here" {
			t.Fatalf("expected match line first with linesPerResult<=2, got %q", sections[0].Snippet)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 total lines, got %d: %q", len(got), sections[0].Snippet)
		}
	})
}

func TestWindowSize(t *testing.T) {
	cases := []struct {
		linesPerResult, wantBefore, wantAfter int
	}{
		{1, 0, 0},
		{2, 0, 1},
		{3, 1, 1},
		{4, 1, 2},
		{10, 1, 8},
	}
	for _, c := range cases {
		before, after := windowSize(c.linesPerResult)
		if before != c.wantBefore || after != c.wantAfter {
			t.Fatalf("windowSize(%d) = (%d,%d), want (%d,%d)", c.linesPerResult, before, after, c.wantBefore, c.wantAfter)
		}
	}
}

// TestSearchFiles_HistoryCollapsesManyVersionsOfSameFileToOneResult is the scenario that
// motivated dropping score-based collapsing entirely: a file with many near-duplicate
// matching FileVersion rows must not crowd out a distinct second file just because a
// flat LIMIT would otherwise fill up on repeats of the first.
func TestSearchFiles_HistoryCollapsesManyVersionsOfSameFileToOneResult(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 5; i++ {
		v := FileVersion{FileId: "manyversions", Path: "many.txt", Content: fmt.Sprintf("needle version %d", i), VersionId: fmt.Sprintf("v%d", i), Date: int64(i), Term: 1}
		if err := db.InsertFileVersion(v); err != nil {
			t.Fatalf("InsertFileVersion: %v", err)
		}
	}
	if err := db.InsertFileVersion(FileVersion{FileId: "single", Path: "single.txt", Content: "needle once", VersionId: "v0", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeHistory = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected exactly 2 distinct FileIds despite 5 matching versions of one file, got %+v", results)
	}
	if _, found := resultByFileId(results, "manyversions"); !found {
		t.Fatalf("expected the many-versions file represented once, got %+v", results)
	}
	if _, found := resultByFileId(results, "single"); !found {
		t.Fatalf("expected the single-version file also found, got %+v", results)
	}
}

// TestSearchFiles_TrashFillsRemainingSlotsBeforeHistory locks in the cascade priority:
// Trash is queried (and gets first claim on remaining slots) before History.
func TestSearchFiles_TrashFillsRemainingSlotsBeforeHistory(t *testing.T) {
	db := newTestDB(t)
	trashTestFile(t, db, "trashed.txt", "needle in trash", 1000)
	if err := db.InsertFileVersion(FileVersion{FileId: "historied", Path: "h.txt", Content: "needle in history", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeTrash = true
	opts.IncludeHistory = true
	opts.MaxFiles = 1
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result given MaxFiles=1, got %+v", results)
	}
	if results[0].Source != "trash" {
		t.Fatalf("expected Trash to win the single remaining slot over History, got %+v", results[0])
	}
}

// TestSearchFiles_HistoryExcludesFileIdAlreadyClaimedByTrash covers a file that was
// edited (leaving FileVersion rows) and later trashed: it must surface once, not twice.
func TestSearchFiles_HistoryExcludesFileIdAlreadyClaimedByTrash(t *testing.T) {
	db := newTestDB(t)
	fileId := trashTestFile(t, db, "both.txt", "needle current", 1000)
	if err := db.InsertFileVersion(FileVersion{FileId: fileId, Path: "both.txt", Content: "needle past version", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeTrash = true
	opts.IncludeHistory = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	r, found := resultByFileId(results, fileId)
	if !found {
		t.Fatalf("expected a result for the trashed-and-historied file, got %+v", results)
	}
	matches := 0
	for _, res := range results {
		if res.FileId == fileId {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly 1 result for a FileId that's both trashed and historied, got %d: %+v", matches, results)
	}
	if r.Source != "trash" {
		t.Fatalf("expected the Trash result to win since Trash is queried before History, got %+v", r)
	}
}

// TestSearchFiles_FilesAloneFillingMaxFilesSkipsOtherSources ensures Trash/History never
// contribute once Files alone has already exhausted the budget.
func TestSearchFiles_FilesAloneFillingMaxFilesSkipsOtherSources(t *testing.T) {
	db := newTestDB(t)
	fileId, err := db.insertFileRecord("a.txt", "needle live", 0)
	if err != nil {
		t.Fatalf("insertFileRecord: %v", err)
	}
	db.flushFts()
	trashTestFile(t, db, "trashed.txt", "needle trashed", 1000)
	if err := db.InsertFileVersion(FileVersion{FileId: "historied", Path: "h.txt", Content: "needle historied", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeTrash = true
	opts.IncludeHistory = true
	opts.MaxFiles = 1
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 1 || results[0].FileId != fileId || results[0].Source != "file" {
		t.Fatalf("expected only the live file result when it alone fills MaxFiles, got %+v", results)
	}
}

// resultByFileId is a small test helper for asserting on one result out of a SearchFiles response.
func resultByFileId(results []SearchResult, fileId string) (SearchResult, bool) {
	for _, r := range results {
		if r.FileId == fileId {
			return r, true
		}
	}
	return SearchResult{}, false
}

func TestSearchFiles_LiveFileOnlyByDefault(t *testing.T) {
	db := newTestDB(t)
	fileId, err := db.insertFileRecord("a.txt", "needle here", 0)
	if err != nil {
		t.Fatalf("insertFileRecord: %v", err)
	}
	db.flushFts()
	if err := db.InsertFileVersion(FileVersion{FileId: "history-only", Path: "h.txt", Content: "needle in history", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	results, err := db.SearchFiles([]string{"needle"}, testSearchOptions())
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected only the live file match with History/Trash excluded by default, got %+v", results)
	}
	if results[0].FileId != fileId || results[0].Source != "file" {
		t.Fatalf("expected live file result, got %+v", results[0])
	}
}

func TestSearchFiles_IncludeHistorySurfacesOrphanedMatch(t *testing.T) {
	db := newTestDB(t)
	// A FileVersion row with no corresponding live `files` row simulates a deleted
	// file whose history outlived it.
	if err := db.InsertFileVersion(FileVersion{FileId: "gone", Path: "old/path.txt", Content: "needle in old version", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeHistory = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	r, found := resultByFileId(results, "gone")
	if !found {
		t.Fatalf("expected orphaned history match included, got %+v", results)
	}
	if r.Source != "history" || r.VersionId != "v1" {
		t.Fatalf("expected history source with versionId v1, got %+v", r)
	}
	if r.Exists {
		t.Fatalf("expected Exists=false for an orphaned file, got %+v", r)
	}
	if r.Path != "old/path.txt" {
		t.Fatalf("expected fallback to the FileVersion's stored path, got %+v", r)
	}
}

func TestSearchFiles_HistoryUsesLivePathWhenFileStillExists(t *testing.T) {
	db := newTestDB(t)
	fileId, err := db.insertFileRecord("new/path.txt", "no match in current content", 0)
	if err != nil {
		t.Fatalf("insertFileRecord: %v", err)
	}
	db.flushFts()
	if err := db.InsertFileVersion(FileVersion{FileId: fileId, Path: "old/path.txt", Content: "needle in an old version", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeHistory = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	r, found := resultByFileId(results, fileId)
	if !found {
		t.Fatalf("expected history match for still-live file, got %+v", results)
	}
	if !r.Exists {
		t.Fatalf("expected Exists=true, got %+v", r)
	}
	if r.Path != "new/path.txt" {
		t.Fatalf("expected current live path rather than the stale historical path, got %+v", r)
	}
}

func TestSearchFiles_LiveMatchSuppressesHistoryAndTrashForSameFile(t *testing.T) {
	db := newTestDB(t)
	fileId, err := db.insertFileRecord("a.txt", "needle in current content", 0)
	if err != nil {
		t.Fatalf("insertFileRecord: %v", err)
	}
	db.flushFts()
	if err := db.InsertFileVersion(FileVersion{FileId: fileId, Path: "a.txt", Content: "needle in old version too", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	opts := testSearchOptions()
	opts.IncludeHistory = true
	opts.IncludeTrash = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one result for the file despite a matching History row, got %+v", results)
	}
	if results[0].Source != "file" {
		t.Fatalf("expected the surviving result to be the live-file source, got %+v", results[0])
	}
}

func TestSearchFiles_IncludeTrashSurfacesDeletedFile(t *testing.T) {
	db := newTestDB(t)
	fileId := trashTestFile(t, db, "deleted.txt", "needle in trashed content", 1000)

	opts := testSearchOptions()
	opts.IncludeTrash = true
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	r, found := resultByFileId(results, fileId)
	if !found {
		t.Fatalf("expected trashed file match, got %+v", results)
	}
	if r.Source != "trash" || r.Exists {
		t.Fatalf("expected trash source with Exists=false, got %+v", r)
	}
	if r.Path != "deleted.txt" {
		t.Fatalf("expected the trash row's stored path, got %+v", r)
	}
}

func TestSearchFiles_TrashExcludedWhenToggleOff(t *testing.T) {
	db := newTestDB(t)
	fileId := trashTestFile(t, db, "deleted.txt", "needle in trashed content", 1000)

	results, err := db.SearchFiles([]string{"needle"}, testSearchOptions())
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if _, found := resultByFileId(results, fileId); found {
		t.Fatalf("expected trash result excluded when IncludeTrash is false, got %+v", results)
	}
}

func TestSearchFiles_RespectsMaxFiles(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 5; i++ {
		if _, err := db.insertFileRecord(strings.Repeat("x", i+1)+".txt", "needle", i); err != nil {
			t.Fatalf("insertFileRecord: %v", err)
		}
	}
	db.flushFts()

	opts := testSearchOptions()
	opts.MaxFiles = 2
	results, err := db.SearchFiles([]string{"needle"}, opts)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected results capped at MaxFiles=2, got %d: %+v", len(results), results)
	}
}

func TestSearchFiles_EmptyTermsReturnsNil(t *testing.T) {
	db := newTestDB(t)
	results, err := db.SearchFiles(nil, testSearchOptions())
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty terms, got %+v", results)
	}
}
