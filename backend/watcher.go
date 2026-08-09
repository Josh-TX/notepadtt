package backend

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	root    string
	db      *DB
	hub     *Hub
	w       *fsnotify.Watcher
	mu      sync.Mutex
	pending map[string]string // oldPath -> fileId (for rename correlation)
}

func StartWatcher(root string, db *DB, hub *Hub) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	wt := &Watcher{root: root, db: db, hub: hub, w: w, pending: map[string]string{}}

	// add root and all subdirs, following directory symlinks
	wt.addTreeWatches(root)

	go wt.loop()
	return nil
}

func (wt *Watcher) loop() {
	for {
		select {
		case event, ok := <-wt.w.Events:
			if !ok {
				return
			}
			wt.handle(event)
		case err, ok := <-wt.w.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func (wt *Watcher) handle(event fsnotify.Event) {
	path := event.Name
	rel, err := filepath.Rel(wt.root, path)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	//log.Printf("[watcher] raw event: op=%s path=%q", event.Op, rel)

	if strings.HasPrefix(rel, dbFileName) {
		return
	}

	switch {
	case event.Has(fsnotify.Create):
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if info.IsDir() {
			// follow symlinks and pick up any pre-existing nested content
			wt.addTreeWatches(path)
			if isSymlink(path) {
				// A real directory copied in fires its own Create event for each
				// nested file as the OS writes them, so those get tracked via the
				// non-dir case below. A symlink is created atomically - nothing
				// else will fire for files that already exist at its target, so
				// scan it now to pick them up.
				wt.trackExistingFiles(path)
			}
			// check for pending rename that created this dir - not common, skip
			wt.broadcastTree("watcher: dir created")
			return
		}
		// check if this is the destination of a pending rename
		wt.mu.Lock()
		matched := false
		for oldPath, fileId := range wt.pending {
			wt.db.UpdatePathById(fileId, rel)
			delete(wt.pending, oldPath)
			matched = true
			break
		}
		wt.mu.Unlock()
		if !matched {
			if wt.hub.RecentlyCreated.Contains(rel) {
				// An API handler already wrote this file and is tracking it in the DB.
				return
			}
			if wt.db.IsTracked(rel) {
				// An atomic save (temp file + rename) landed on an already-tracked
				// path from an untracked source name, so it never matched wt.pending
				// above. Treat it like a Write: re-read and broadcast the new content.
				wt.syncContent(rel, path)
			} else if wt.db.IsAllowedPath(rel) {
				wt.db.EnsureFileTracked(rel)
			}
		}
		wt.broadcastTree("watcher: file created or rename dest")

	case event.Has(fsnotify.Write):
		wt.syncContent(rel, path)

	case event.Has(fsnotify.Rename):
		f, _ := wt.db.GetFileByPath(rel)
		if f != nil {
			wt.mu.Lock()
			wt.pending[rel] = f.FileId
			wt.mu.Unlock()
			// clear after short window
			go func(oldPath string) {
				time.Sleep(200 * time.Millisecond)
				wt.mu.Lock()
				_, stillPending := wt.pending[oldPath]
				if stillPending {
					delete(wt.pending, oldPath)
				}
				wt.mu.Unlock()
				if stillPending {
					wt.broadcastTree("watcher: rename timeout (file moved out or deleted)")
				}
			}(rel)
		}

	case event.Has(fsnotify.Remove):
		if f, _ := wt.db.GetFileByPath(rel); f != nil {
			wt.db.TrashFile(f.FileId, f.Path, f.Content, time.Now().UnixMilli())
			wt.broadcastTree("watcher: file removed")
		}
	}
}

// syncContent re-reads path from disk and, if its content differs from what's
// tracked for rel, records a version and broadcasts the update. Used for both
// plain Write events and Create events that turn out to be an atomic-save
// rename landing on an already-tracked path. If the write grew the file past
// MaxFileSizeKB, it's untracked entirely instead (like any other deletion).
func (wt *Watcher) syncContent(rel, path string) {
	f, _ := wt.db.GetFileByPath(rel)
	if f == nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !withinMaxFileSize(info.Size()) {
		wt.db.TrashFile(f.FileId, f.Path, f.Content, time.Now().UnixMilli())
		wt.broadcastTree("watcher: file grew past max size, untracked")
		return
	}
	b, _ := os.ReadFile(path)
	content := string(b)
	if content == f.Content {
		return
	}
	wt.hub.Versions.Add(f.FileId, f.VersionId, f.Path, f.Content)
	versionId := uniqueId(5)
	wt.db.UpdateContentAndVersion(f.FileId, content, versionId)
	wt.hub.Versions.Add(f.FileId, versionId, f.Path, content)
	wt.hub.BroadcastContent(f.FileId, content, versionId, "", "watcher: file written on disk")
}

// addTreeWatches adds a watch on dir and recurses into its entries, following
// directory symlinks (see walkFollowSymlinks).
func (wt *Watcher) addTreeWatches(dir string) {
	walkFollowSymlinks(dir, func(d string) error {
		wt.w.Add(d)
		return nil
	}, nil)
}

// trackExistingFiles walks dir (following symlinks) and tracks any allowed
// file that isn't already in the DB, so pre-existing content revealed by a
// newly created symlink shows up immediately instead of only after restart.
func (wt *Watcher) trackExistingFiles(dir string) {
	walkFollowSymlinks(dir, nil, func(path string) error {
		rel, err := filepath.Rel(wt.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if wt.db.IsAllowedPath(rel) && !wt.db.IsTracked(rel) {
			wt.db.EnsureFileTracked(rel)
		}
		return nil
	})
}

func (wt *Watcher) broadcastTree(reason string) {
	files, err := wt.db.GetAllFilesOnDisk()
	if err != nil {
		return
	}
	wt.hub.BroadcastFS(BuildTree(files, wt.root), reason)
}
