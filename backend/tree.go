package backend

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileNode struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsLink bool   `json:"isLink"`
}

type FolderNode struct {
	Name    string        `json:"name"`
	Path    string        `json:"path"`
	Files   []*FileNode   `json:"files"`
	Folders []*FolderNode `json:"folders"`
	IsLink  bool          `json:"isLink"`
}

// BuildTree walks rootAbs (following directory symlinks, skipping excluded names)
// and returns the folder tree: folders first, then files, each case-insensitive
// alphabetical.
func BuildTree(rootAbs string) *FolderNode {
	root := &FolderNode{Files: []*FileNode{}, Folders: []*FolderNode{}}
	nodes := map[string]*FolderNode{"": root}
	walkFollowSymlinks(rootAbs, func(dir string) error {
		rel, err := filepath.Rel(rootAbs, dir)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		parent := nodes[parentOf(rel)]
		if parent == nil {
			return nil
		}
		n := &FolderNode{Name: path.Base(rel), Path: rel, Files: []*FileNode{}, Folders: []*FolderNode{}, IsLink: isSymlink(dir)}
		nodes[rel] = n
		parent.Folders = append(parent.Folders, n)
		return nil
	}, func(file string) error {
		rel, err := filepath.Rel(rootAbs, file)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		parent := nodes[parentOf(rel)]
		if parent == nil {
			return nil
		}
		parent.Files = append(parent.Files, &FileNode{Name: path.Base(rel), Path: rel, IsLink: isSymlink(file)})
		return nil
	})
	sortTree(root)
	return root
}

func parentOf(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// isSymlink reports whether p itself (not its target) is a symlink.
func isSymlink(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func sortTree(n *FolderNode) {
	less := func(a, b string) bool {
		la, lb := strings.ToLower(a), strings.ToLower(b)
		if la != lb {
			return la < lb
		}
		return a < b
	}
	sort.Slice(n.Folders, func(i, j int) bool { return less(n.Folders[i].Name, n.Folders[j].Name) })
	sort.Slice(n.Files, func(i, j int) bool { return less(n.Files[i].Name, n.Files[j].Name) })
	for _, f := range n.Folders {
		sortTree(f)
	}
}

const (
	treeDebounce = 50 * time.Millisecond
	treeMaxWait  = time.Second
)

// scheduleTree debounces a rebuild + broadcast of the file tree. Events keep
// pushing the rebuild back, but never by more than treeMaxWait in total, so a
// continuous stream of changes can't starve updates.
func (s *Server) scheduleTree() {
	s.treeMu.Lock()
	defer s.treeMu.Unlock()
	now := time.Now()
	if s.treeTimer == nil {
		s.treeFirst = now
		s.treeTimer = time.AfterFunc(treeDebounce, s.rebuildTree)
		return
	}
	d := min(treeDebounce, max(treeMaxWait-now.Sub(s.treeFirst), 0))
	s.treeTimer.Reset(d)
}

func (s *Server) rebuildTree() {
	s.treeMu.Lock()
	s.treeTimer = nil
	s.treeMu.Unlock()
	s.hub.BroadcastFS(BuildTree(s.rootAbs))
}
