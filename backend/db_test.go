package backend

import (
	"os"
	"path/filepath"
	"testing"
)

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
