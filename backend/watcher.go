package backend

import (
	"io/fs"
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

	// add root and all subdirs
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			w.Add(path)
		}
		return nil
	})

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

	if strings.HasPrefix(rel, ".notepadtt.db") {
		return
	}

	switch {
	case event.Has(fsnotify.Create):
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if info.IsDir() {
			wt.w.Add(path)
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
			if wt.db.IsAllowedPath(rel) || wt.db.IsTracked(rel) {
				wt.db.EnsureFileTracked(rel)
			}
		}
		wt.broadcastTree("watcher: file created or rename dest")

	case event.Has(fsnotify.Write):
		f, _ := wt.db.GetFileByPath(rel)
		if f == nil {
			return
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if string(content) == f.Content {
			return
		}
		wt.hub.Versions.Add(f.FileId, f.VersionId, f.Path, f.Content)
		versionId := uniqueId(5)
		wt.db.UpdateContentAndVersion(f.FileId, string(content), versionId)
		wt.hub.Versions.Add(f.FileId, versionId, f.Path, string(content))
		wt.hub.BroadcastContent(f.FileId, string(content), versionId, "", "watcher: file written on disk")

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

func (wt *Watcher) broadcastTree(reason string) {
	files, err := wt.db.GetAllFilesOnDisk()
	if err != nil {
		return
	}
	wt.hub.BroadcastFS(BuildTree(files, wt.root), reason)
}
