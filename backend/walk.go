package backend

import (
	"io/fs"
	"os"
	"path/filepath"
)

// walkFollowSymlinks recursively visits root and its descendants like
// filepath.WalkDir, but also descends into directory symlinks (filepath.WalkDir
// treats symlinks as opaque leaf entries and never follows them). Resolved
// real paths are tracked so symlink cycles (e.g. a dir symlinked to an
// ancestor) terminate instead of recursing forever.
//
// onDir is called for every directory, including root and symlinked ones,
// before its contents are read; returning fs.SkipDir skips that directory's
// contents without stopping the rest of the walk, any other non-nil error
// aborts the walk. onFile is called for every non-directory entry (including
// symlinks to files). Either callback may be nil.
func walkFollowSymlinks(root string, onDir func(path string) error, onFile func(path string) error) error {
	visited := map[string]bool{}
	var walk func(dir string) error
	walk = func(dir string) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil
		}
		alreadyVisited := visited[real]
		if onDir != nil {
			if err := onDir(dir); err != nil {
				if err == fs.SkipDir {
					return nil
				}
				return err
			}
		}
		if alreadyVisited {
			// dir closes a symlink cycle back to an ancestor: report it once
			// (above) but don't descend again, or the walk would never end.
			return nil
		}
		visited[real] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			childPath := filepath.Join(dir, e.Name())
			isDir := e.IsDir()
			if !isDir && e.Type()&os.ModeSymlink != 0 {
				if info, statErr := os.Stat(childPath); statErr == nil && info.IsDir() {
					isDir = true
				}
			}
			if isDir {
				if err := walk(childPath); err != nil {
					return err
				}
				continue
			}
			if onFile != nil {
				if err := onFile(childPath); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root)
}
