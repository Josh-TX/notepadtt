package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A folder inside the workspace root that's actually a symlink to a directory
// elsewhere on disk should still be watched, including its nested subdirs, so
// that host edits to files reached through the symlink are picked up.
func TestWatcher_FollowsFolderSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()

	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	notePath := filepath.Join(target, "nested", "note.txt")
	if err := os.WriteFile(notePath, []byte("before"), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	hub := NewHub()
	fileId, err := db.EnsureFileTracked("linked/nested/note.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}

	if err := StartWatcher(root, db, hub); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	// host edits the file through the real (non-symlinked) path
	if err := os.WriteFile(notePath, []byte("after"), 0644); err != nil {
		t.Fatalf("rewrite note: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f, err := db.GetFile(fileId)
		if err != nil {
			t.Fatalf("GetFile: %v", err)
		}
		if f.Content == "after" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("db content was not updated after host edit through symlinked folder")
}
