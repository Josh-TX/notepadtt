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

// A pre-existing file reached only through a folder symlink (i.e. present on
// disk before the app ever started, so no fsnotify Create event will fire for
// it) should still be discovered and tracked by the startup scan.
func TestStartupScan_DiscoversFileThroughFolderSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()

	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	notePath := filepath.Join(target, "nested", "note.txt")
	if err := os.WriteFile(notePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	f, err := db.GetFileByPath("linked/nested/note.txt")
	if err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	}
	if f == nil {
		t.Fatalf("file reached through symlinked folder was not tracked by startup scan")
	}
	if f.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", f.Content)
	}
}

// A file exceeding MaxFileSizeKB should still be tracked (visible in the FileTree) so
// e.g. a large video dropped into the workspace shows up, but its content should never
// be read into the DB.
func TestScan_OversizedFileTrackedButContentNotStored(t *testing.T) {
	root := t.TempDir()
	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	settings := defaultSettings()
	settings.MaxFileSizeKB = 1
	if err := setSettingsCache(settings); err != nil {
		t.Fatalf("setSettingsCache: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "big.txt"), make([]byte, 2000), 0644); err != nil {
		t.Fatalf("write big.txt: %v", err)
	}
	if err := db.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	f, err := db.GetFileByPath("big.txt")
	if err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	}
	if f == nil {
		t.Fatalf("oversized file should still be tracked")
	}
	if f.Content != "" {
		t.Fatalf("expected empty content for oversized file, got %d bytes", len(f.Content))
	}
}
