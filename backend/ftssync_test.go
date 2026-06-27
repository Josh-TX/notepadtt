package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := NewDB(dir)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	return db
}

func searchFor(t *testing.T, db *DB, term string) []SearchResult {
	t.Helper()
	results, err := db.SearchFiles([]string{term}, testSearchOptions())
	if err != nil {
		t.Fatalf("SearchFiles(%q): %v", term, err)
	}
	return results
}

// testSearchOptions mirrors defaultSettings()'s search-related fields, for tests that
// don't care about exercising non-default LinesPerResult/MaxResultsPerFile/MaxFiles.
func testSearchOptions() SearchOptions {
	return SearchOptions{LinesPerResult: 4, MaxResultsPerFile: 2, MaxFiles: 30}
}

// A freshly created file is invisible to search until flushFts runs (the debounce
// hasn't fired yet), then becomes findable once flushed.
func TestFtsSync_InsertThenFlush(t *testing.T) {
	db := newTestDB(t)
	if err := os.WriteFile(filepath.Join(db.root, "alpha.txt"), []byte("uniquetokenzz"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked("alpha.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}

	if got := searchFor(t, db, "uniquetokenzz"); len(got) != 0 {
		t.Fatalf("expected no results before flush, got %+v", got)
	}

	db.flushFts()

	got := searchFor(t, db, "uniquetokenzz")
	if len(got) != 1 || got[0].FileId != fileId {
		t.Fatalf("expected 1 result for fileId %s after flush, got %+v", fileId, got)
	}
}

// Regression test for the spec's core invariant: at flush time, files.Content already
// holds the newest value, so the FTS5 'delete' command must use the cache's old value,
// not a fresh read of files. Multiple content changes collapse into a single
// delete(old-cached)+insert(final) pair when only one flush happens for the batch.
func TestFtsSync_UpdateUsesCachedOldValueNotLatest(t *testing.T) {
	db := newTestDB(t)
	if err := os.WriteFile(filepath.Join(db.root, "notes.txt"), []byte("aaaa"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked("notes.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	db.flushFts() // now indexed & cached as "aaaa"

	// Two updates land before the next flush; only the final value should ever be
	// the one that actually gets indexed.
	db.UpdateContentAndVersion(fileId, "bbbb", "v1")
	db.UpdateContentAndVersion(fileId, "cccc", "v2")
	db.flushFts()

	if got := searchFor(t, db, "aaaa"); len(got) != 0 {
		t.Fatalf("old content should have been removed from the index, got %+v", got)
	}
	if got := searchFor(t, db, "bbbb"); len(got) != 0 {
		t.Fatalf("intermediate content should never have been indexed, got %+v", got)
	}
	got := searchFor(t, db, "cccc")
	if len(got) != 1 || got[0].FileId != fileId {
		t.Fatalf("expected final content findable, got %+v", got)
	}
}

// Renaming a file changes its Path, which is itself an indexed FTS column.
func TestFtsSync_RenameUpdatesIndexedPath(t *testing.T) {
	db := newTestDB(t)
	if err := os.WriteFile(filepath.Join(db.root, "oldname.txt"), []byte("body"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked("oldname.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	db.flushFts()

	db.UpdatePathById(fileId, "newname.txt")
	db.flushFts()

	if got := searchFor(t, db, "oldname"); len(got) != 0 {
		t.Fatalf("old path should no longer match, got %+v", got)
	}
	got := searchFor(t, db, "newname")
	if len(got) != 1 || got[0].FileId != fileId {
		t.Fatalf("expected new path findable, got %+v", got)
	}
}

// Deleting a file removes it from the index and drops its cache entry rather than
// leaving orphaned postings behind.
func TestFtsSync_DeleteRemovesFromIndex(t *testing.T) {
	db := newTestDB(t)
	if err := os.WriteFile(filepath.Join(db.root, "gone.txt"), []byte("vanishingcontent"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked("gone.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	db.flushFts()
	if got := searchFor(t, db, "vanishingcontent"); len(got) != 1 {
		t.Fatalf("expected to find file before delete, got %+v", got)
	}

	db.DeleteFile(fileId)
	db.flushFts()

	if got := searchFor(t, db, "vanishingcontent"); len(got) != 0 {
		t.Fatalf("expected no results after delete, got %+v", got)
	}
}

// seedFtsCache (called after startupScan/server restart) repopulates the in-memory
// cache from files without touching files_fts itself; a subsequent edit must still
// correctly remove the prior indexed value rather than leaving it as an orphan.
func TestFtsSync_SeedCacheThenEditStillCleansUpOldValue(t *testing.T) {
	db := newTestDB(t)
	if err := os.WriteFile(filepath.Join(db.root, "seeded.txt"), []byte("origcontent"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked("seeded.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	db.flushFts()

	if err := db.seedFtsCache(); err != nil {
		t.Fatalf("seedFtsCache: %v", err)
	}

	db.UpdateContentAndVersion(fileId, "newcontent", "v1")
	db.flushFts()

	if got := searchFor(t, db, "origcontent"); len(got) != 0 {
		t.Fatalf("expected old content cleaned up after reseed+edit, got %+v", got)
	}
	got := searchFor(t, db, "newcontent")
	if len(got) != 1 || got[0].FileId != fileId {
		t.Fatalf("expected new content findable, got %+v", got)
	}
}
