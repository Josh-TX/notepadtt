package backend

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (s *Server) startWatcher() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.addTreeWatches(w, s.rootAbs)
	go func() {
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				s.handleFSEvent(w, ev)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("watcher error: %v", err)
			}
		}
	}()
	return nil
}

// addTreeWatches watches dir and everything beneath it, following directory
// symlinks and skipping excluded names.
func (s *Server) addTreeWatches(w *fsnotify.Watcher, dir string) {
	walkFollowSymlinks(dir, func(d string) error {
		if err := w.Add(d); err != nil {
			log.Printf("watcher: add %s: %v", d, err)
		}
		return nil
	}, nil)
}

func (s *Server) handleFSEvent(w *fsnotify.Watcher, ev fsnotify.Event) {
	rel, err := filepath.Rel(s.rootAbs, ev.Name)
	if err != nil || rel == "." {
		return
	}
	rel = filepath.ToSlash(rel)
	for _, part := range strings.Split(rel, "/") {
		if isExcludedName(part) {
			return
		}
	}

	switch {
	case ev.Has(fsnotify.Create):
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			s.addTreeWatches(w, ev.Name)
		} else {
			// Atomic saves (temp file + rename) land as a Create on an open path.
			s.syncOpenPath(rel)
		}
		s.scheduleTree()
	case ev.Has(fsnotify.Write):
		s.syncOpenPath(rel)
	case ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
		s.scheduleTree()
		// Give an atomic-save rename a moment to land before deciding it's gone.
		time.AfterFunc(150*time.Millisecond, func() { s.closeIfGone(rel) })
	}
}

func (s *Server) syncOpenPath(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.byPath[rel]; f != nil {
		s.syncFromDiskLocked(f)
	}
}

// closeIfGone closes the tabs at rel (or beneath it, if rel was a folder) whose
// files no longer exist on disk.
func (s *Server) closeIfGone(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	closed := false
	for _, f := range s.tabsUnderLocked(rel) {
		if _, err := os.Stat(s.abs(f.path)); err != nil {
			s.closeTabLocked(f.id)
			closed = true
		}
	}
	if closed {
		s.broadcastTabsLocked()
	}
}
