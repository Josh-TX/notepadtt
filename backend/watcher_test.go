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

// Creating a new symlink to a folder while the server is running should
// immediately track any pre-existing files reached through it, not just wait
// for a future restart's startupScan.
func TestWatcher_TracksExistingFilesInNewlyCreatedSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()

	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	notePath := filepath.Join(target, "nested", "note.txt")
	if err := os.WriteFile(notePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	hub := NewHub()
	if err := StartWatcher(root, db, hub); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	// symlink created live, after the watcher is already running
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f, _ := db.GetFileByPath("linked/nested/note.txt")
		if f != nil && f.Content == "hello" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file inside newly created symlinked folder was not tracked")
}

// Editors commonly save by writing a new temp file and renaming it over the
// original (atomic save), so the writer's random temp name never appears in
// the DB. The rename's destination is an already-tracked path, so it must be
// picked up as a content update rather than silently ignored.
func TestWatcher_AtomicSaveRenameOverTrackedFile(t *testing.T) {
	root := t.TempDir()
	notePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(notePath, []byte("before"), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	db, err := NewDB(root)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	hub := NewHub()
	fileId, err := db.EnsureFileTracked("note.txt")
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}

	if err := StartWatcher(root, db, hub); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	tmp, err := os.CreateTemp(root, "note.txt.tmp*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString("after"); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	tmp.Close()
	if err := os.Rename(tmpPath, notePath); err != nil {
		t.Fatalf("rename: %v", err)
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
	t.Fatalf("db content was not updated after atomic-save rename over tracked file")
}
