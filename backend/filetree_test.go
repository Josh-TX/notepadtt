package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A folder inside the workspace root that's actually a symlink to a directory
// elsewhere on disk should appear in the tree even when it has no tracked
// files yet (e.g. right after the symlink is created, before any file inside
// it gets opened).
func TestBuildTree_IncludesEmptyFolderSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()

	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	tree := BuildTree(nil, root)

	var linked *FolderNode
	for i := range tree.Folders {
		if tree.Folders[i].Name == "linked" {
			linked = &tree.Folders[i]
		}
	}
	if linked == nil {
		t.Fatalf("expected symlinked folder %q in tree, got folders: %+v", "linked", tree.Folders)
	}
	var nested *FolderNode
	for i := range linked.Folders {
		if linked.Folders[i].Name == "nested" {
			nested = &linked.Folders[i]
		}
	}
	if nested == nil {
		t.Fatalf("expected nested folder inside symlinked folder, got: %+v", linked.Folders)
	}
}

// A folder symlinked to itself (or to an ancestor) must not send BuildTree
// into infinite recursion.
func TestBuildTree_HandlesSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "self")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	done := make(chan FolderNode, 1)
	go func() {
		done <- BuildTree(nil, root)
	}()

	select {
	case tree := <-done:
		found := false
		for _, f := range tree.Folders {
			if f.Name == "self" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected self-referential symlink folder in tree, got: %+v", tree.Folders)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("BuildTree did not terminate on a symlink cycle")
	}
}
