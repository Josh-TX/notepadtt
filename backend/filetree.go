package backend

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileNode struct {
	FileId     string `json:"fileId"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	LastOpened int64  `json:"lastOpened"`
	OrderNum   int    `json:"orderNum"`
	IsLink     bool   `json:"isLink"`
}

type FolderNode struct {
	Name    string       `json:"name"`
	Path    string       `json:"path"`
	Files   []FileNode   `json:"files"`
	Folders []FolderNode `json:"folders"`
	IsLink  bool         `json:"isLink"`
}

func BuildTree(files []DBFile, rootDir string) FolderNode {
	root := FolderNode{Name: "", Path: "", Files: []FileNode{}, Folders: []FolderNode{}}
	for _, f := range files {
		insertIntoTree(&root, f, rootDir)
	}
	walkFollowSymlinks(rootDir, func(path string) error {
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(filepath.Base(path), ".") {
			return fs.SkipDir
		}
		ensureFolderInTree(&root, rel, isSymlink(path))
		return nil
	}, nil)
	sortTree(&root)
	return root
}

// isSymlink reports whether path itself (not its target) is a symlink.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// ensureFolderInTree walks/creates the folder chain for relPath. isLink describes
// only the deepest (final) segment - the actual directory walkFollowSymlinks
// visited - since intermediate ancestors implicitly created along the way are
// always real directories reached earlier in the same walk.
func ensureFolderInTree(node *FolderNode, relPath string, isLink bool) {
	parts := strings.SplitN(relPath, "/", 2)
	subName := parts[0]
	var subPath string
	if node.Path == "" {
		subPath = subName
	} else {
		subPath = node.Path + "/" + subName
	}
	var sub *FolderNode
	for i := range node.Folders {
		if node.Folders[i].Path == subPath {
			sub = &node.Folders[i]
			break
		}
	}
	if sub == nil {
		node.Folders = append(node.Folders, FolderNode{Name: subName, Path: subPath, Files: []FileNode{}, Folders: []FolderNode{}})
		sub = &node.Folders[len(node.Folders)-1]
	}
	if len(parts) > 1 {
		ensureFolderInTree(sub, parts[1], isLink)
	} else {
		sub.IsLink = isLink
	}
}


func insertIntoTree(node *FolderNode, f DBFile, rootDir string) {
	dir := filepath.Dir(f.Path)
	if dir == "." {
		dir = ""
	}

	if dir == node.Path {
		node.Files = append(node.Files, FileNode{
			FileId:     f.FileId,
			Name:       filepath.Base(f.Path),
			Path:       f.Path,
			LastOpened: f.LastOpened,
			OrderNum:   f.OrderNum,
			IsLink:     isSymlink(filepath.Join(rootDir, filepath.FromSlash(f.Path))),
		})
		return
	}

	// find or create the next subfolder segment
	rel := strings.TrimPrefix(f.Path, node.Path)
	if node.Path != "" {
		rel = strings.TrimPrefix(rel, "/")
	}
	parts := strings.SplitN(rel, "/", 2)
	subName := parts[0]
	var subPath string
	if node.Path == "" {
		subPath = subName
	} else {
		subPath = node.Path + "/" + subName
	}

	for i := range node.Folders {
		if node.Folders[i].Path == subPath {
			insertIntoTree(&node.Folders[i], f, rootDir)
			return
		}
	}
	newFolder := FolderNode{Name: subName, Path: subPath, Files: []FileNode{}, Folders: []FolderNode{}}
	node.Folders = append(node.Folders, newFolder)
	insertIntoTree(&node.Folders[len(node.Folders)-1], f, rootDir)
}

func sortTree(node *FolderNode) {
	sort.Slice(node.Folders, func(i, j int) bool {
		return strings.ToLower(node.Folders[i].Name) < strings.ToLower(node.Folders[j].Name)
	})
	sort.Slice(node.Files, func(i, j int) bool {
		return node.Files[i].OrderNum < node.Files[j].OrderNum
	})
	for i := range node.Folders {
		sortTree(&node.Folders[i])
	}
}
