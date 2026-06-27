package backend

import (
	"io/fs"
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
}

type FolderNode struct {
	Name    string       `json:"name"`
	Path    string       `json:"path"`
	Files   []FileNode   `json:"files"`
	Folders []FolderNode `json:"folders"`
}

func BuildTree(files []DBFile, rootDir string) FolderNode {
	root := FolderNode{Name: "", Path: "", Files: []FileNode{}, Folders: []FolderNode{}}
	for _, f := range files {
		insertIntoTree(&root, f)
	}
	filepath.WalkDir(rootDir, func(path string, de fs.DirEntry, err error) error {
		if err != nil || !de.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(de.Name(), ".") {
			return fs.SkipDir
		}
		ensureFolderInTree(&root, rel)
		return nil
	})
	sortTree(&root)
	return root
}

func ensureFolderInTree(node *FolderNode, relPath string) {
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
		ensureFolderInTree(sub, parts[1])
	}
}


func insertIntoTree(node *FolderNode, f DBFile) {
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
			insertIntoTree(&node.Folders[i], f)
			return
		}
	}
	newFolder := FolderNode{Name: subName, Path: subPath, Files: []FileNode{}, Folders: []FolderNode{}}
	node.Folders = append(node.Folders, newFolder)
	insertIntoTree(&node.Folders[len(node.Folders)-1], f)
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
