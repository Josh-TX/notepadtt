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

// A file exceeding MaxFileSizeKB should be excluded from tracking entirely, the same
// as a disallowed extension: no Files row, so it never shows up in the FileTree.
func TestScan_OversizedFileExcludedFromTracking(t *testing.T) {
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
	if f != nil {
		t.Fatalf("oversized file should not be tracked")
	}
}

// A previously-tracked file that grows past MaxFileSizeKB (e.g. edited on disk while the
// app wasn't running) should be untracked on the next scan, same as a file deleted from
// disk — moved to FileTrash rather than hard-deleted, so it's recoverable if it shrinks
// back down.
func TestScan_PreviouslyTrackedFileTrashedWhenGrownOversized(t *testing.T) {
	root := t.TempDir()
	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("small"), 0644); err != nil {
		t.Fatalf("write note.txt: %v", err)
	}
	if err := db.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	f, err := db.GetFileByPath("note.txt")
	if err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	}
	if f == nil {
		t.Fatalf("note.txt should be tracked before growing oversized")
	}

	settings := defaultSettings()
	settings.MaxFileSizeKB = 1
	if err := setSettingsCache(settings); err != nil {
		t.Fatalf("setSettingsCache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), make([]byte, 2000), 0644); err != nil {
		t.Fatalf("grow note.txt: %v", err)
	}
	if err := db.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got, err := db.GetFileByPath("note.txt"); err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	} else if got != nil {
		t.Fatalf("grown-oversized file should no longer be tracked")
	}
	if _, found, err := db.GetFileTrash(f.FileId); err != nil {
		t.Fatalf("GetFileTrash: %v", err)
	} else if !found {
		t.Fatalf("grown-oversized file should have been moved to FileTrash")
	}
}
