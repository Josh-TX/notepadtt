package backend

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"
)

type closedTab struct {
	fileId string
	path   string
	index  int
}

// trashItem is a deleted file/folder parked in .ntt-trash for trashTTL.
type trashItem struct {
	id       string
	origRel  string
	trashAbs string // dir holding the moved entry
	base     string
	tabs     []closedTab
	timer    *time.Timer
}

// handleDelete moves a file or folder to .ntt-trash, closes any tabs under it, and
// schedules permanent removal after trashTTL. Returns the trash id for restoring.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	decodeBody(r, &body)
	if !validRel(body.Path) {
		http.Error(w, "invalid path", 400)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(s.abs(body.Path)); err != nil {
		http.Error(w, "not found", 404)
		return
	}
	item := &trashItem{id: uniqueId(12), origRel: body.Path, base: path.Base(body.Path)}
	item.trashAbs = filepath.Join(s.rootAbs, trashDirName, item.id)
	if err := os.MkdirAll(item.trashAbs, 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.Rename(s.abs(body.Path), filepath.Join(item.trashAbs, item.base)); err != nil {
		os.RemoveAll(item.trashAbs)
		http.Error(w, err.Error(), 500)
		return
	}

	for _, f := range s.tabsUnderLocked(body.Path) {
		idx := s.closeTabLocked(f.id)
		item.tabs = append(item.tabs, closedTab{fileId: f.id, path: f.path, index: idx})
	}
	if len(item.tabs) > 0 {
		s.broadcastTabsLocked()
	}
	s.trash[item.id] = item
	item.timer = time.AfterFunc(trashTTL, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.trash[item.id] == item {
			delete(s.trash, item.id)
			os.RemoveAll(item.trashAbs)
		}
	})
	s.scheduleTree()
	writeJSON(w, map[string]string{"trashId": item.id})
}

// handleRestore moves a trashed entry back and reopens its tabs at their old
// positions. Fails with 409 if the original path is occupied again.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.trash[id]
	if item == nil {
		http.Error(w, "undo expired", 404)
		return
	}
	if _, err := os.Lstat(s.abs(item.origRel)); err == nil {
		http.Error(w, "can't restore: path exists", 409)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.abs(item.origRel)), 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.Rename(filepath.Join(item.trashAbs, item.base), s.abs(item.origRel)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	item.timer.Stop()
	delete(s.trash, id)
	os.RemoveAll(item.trashAbs)

	sort.Slice(item.tabs, func(i, j int) bool { return item.tabs[i].index < item.tabs[j].index })
	restoredId := ""
	for _, t := range item.tabs {
		if s.byPath[t.path] != nil {
			continue
		}
		f := &openFile{id: t.fileId, path: t.path}
		s.byId[f.id] = f
		s.byPath[f.path] = f
		at := min(max(t.index, 0), len(s.tabs))
		s.tabs = append(s.tabs[:at], append([]string{f.id}, s.tabs[at:]...)...)
		restoredId = f.id
	}
	if len(item.tabs) > 0 {
		s.broadcastTabsLocked()
	}
	s.scheduleTree()
	writeJSON(w, map[string]string{"path": item.origRel, "fileId": restoredId})
}
