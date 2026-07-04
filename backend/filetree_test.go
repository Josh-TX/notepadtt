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

// isLink should be true only for the node that is itself a symlink - a
// symlinked folder is flagged, but a real file/folder reached through it
// (or a real file that's simply the direct child of the workspace root) is not.
func TestBuildTree_IsLinkOnlyOnTheSymlinkItself(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(target, "real.txt"), []byte("hi"), 0644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("hi"), 0644); err != nil {
		t.Fatalf("write plain file: %v", err)
	}
	linkedFilePath := filepath.Join(target, "real.txt")
	if err := os.Symlink(linkedFilePath, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	files := []DBFile{
		{FileId: "f1", Path: "plain.txt"},
		{FileId: "f2", Path: "linked.txt"},
		{FileId: "f3", Path: "linked/real.txt"},
	}
	tree := BuildTree(files, root)

	var linkedFolder *FolderNode
	for i := range tree.Folders {
		if tree.Folders[i].Name == "linked" {
			linkedFolder = &tree.Folders[i]
		}
	}
	if linkedFolder == nil || !linkedFolder.IsLink {
		t.Fatalf("expected folder %q to have IsLink=true, got %+v", "linked", linkedFolder)
	}

	byName := map[string]FileNode{}
	for _, f := range tree.Files {
		byName[f.Name] = f
	}
	for _, f := range linkedFolder.Files {
		byName[f.Name] = f
	}
	if got := byName["plain.txt"]; got.IsLink {
		t.Fatalf("expected plain.txt IsLink=false, got %+v", got)
	}
	if got := byName["linked.txt"]; !got.IsLink {
		t.Fatalf("expected linked.txt IsLink=true, got %+v", got)
	}
	if got := byName["real.txt"]; got.IsLink {
		t.Fatalf("expected real.txt (inside symlinked folder) IsLink=false, got %+v", got)
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
