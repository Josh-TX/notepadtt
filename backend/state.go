package backend

import (
	"os"
	"path/filepath"
	"strings"
)

// openFile is the in-memory identity of a file with a tab. FileIds live only as
// long as the tab (or its trash entry); they survive renames/moves.
type openFile struct {
	id        string
	path      string // slash-separated, relative to root
	loaded    bool   // content/versionId are valid
	content   string
	versionId string
}

type TabInfo struct {
	FileId  string `json:"fileId"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	AbsPath string `json:"absPath"`
}

// validRel reports whether rel is a clean relative path inside root with no
// excluded components. The empty string (root) is not valid here.
func validRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || isExcludedName(part) {
			return false
		}
	}
	return true
}

// validName reports whether name is a legal single path segment.
func validName(name string) bool {
	return name != "" && !strings.Contains(name, "/") && validRel(name)
}

func (s *Server) abs(rel string) string {
	return filepath.Join(s.rootAbs, filepath.FromSlash(rel))
}

// ---- everything below requires s.mu held ----

func (s *Server) tabInfoLocked(f *openFile) TabInfo {
	return TabInfo{FileId: f.id, Path: f.path, Name: filepath.Base(f.path), AbsPath: s.abs(f.path)}
}

func (s *Server) tabsInfoLocked() []TabInfo {
	out := make([]TabInfo, 0, len(s.tabs))
	for _, id := range s.tabs {
		if f := s.byId[id]; f != nil {
			out = append(out, s.tabInfoLocked(f))
		}
	}
	return out
}

func (s *Server) broadcastTabsLocked() {
	s.hub.Broadcast(map[string]any{"type": "tabs", "tabs": s.tabsInfoLocked()})
}

// openTabLocked returns the tab for rel, appending a new one if it isn't open.
func (s *Server) openTabLocked(rel string) *openFile {
	if f := s.byPath[rel]; f != nil {
		return f
	}
	f := &openFile{id: uniqueId(12), path: rel}
	s.byId[f.id] = f
	s.byPath[rel] = f
	s.tabs = append(s.tabs, f.id)
	return f
}

// closeTabLocked removes the tab and forgets its identity, returning its index
// (or -1 if not open).
func (s *Server) closeTabLocked(id string) int {
	f := s.byId[id]
	if f == nil {
		return -1
	}
	idx := -1
	for i, t := range s.tabs {
		if t == id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.tabs = append(s.tabs[:idx], s.tabs[idx+1:]...)
	}
	delete(s.byId, id)
	delete(s.byPath, f.path)
	s.hub.Versions.Forget(id)
	return idx
}

// loadLocked reads f's content from disk and mints a fresh versionId.
func (s *Server) loadLocked(f *openFile) error {
	b, err := os.ReadFile(s.abs(f.path))
	if err != nil {
		return err
	}
	f.content = string(b)
	f.versionId = uniqueId(5)
	f.loaded = true
	s.hub.Versions.Add(f.id, f.versionId, f.content)
	return nil
}

// maybeUnloadLocked drops a large file's content once no client is subscribed.
func (s *Server) maybeUnloadLocked(id string) {
	f := s.byId[id]
	if f == nil || !f.loaded || len(f.content) <= maxDisplayBytes {
		return
	}
	if s.hub.HasSubscribers(id) {
		return
	}
	f.loaded = false
	f.content = ""
}

// repathLocked updates open tabs after oldRel (file or folder) became newRel.
func (s *Server) repathLocked(oldRel, newRel string) {
	var moved []*openFile
	for p, f := range s.byPath {
		if p == oldRel || strings.HasPrefix(p, oldRel+"/") {
			moved = append(moved, f)
		}
	}
	for _, f := range moved {
		delete(s.byPath, f.path)
	}
	for _, f := range moved {
		f.path = newRel + strings.TrimPrefix(f.path, oldRel)
		s.byPath[f.path] = f
	}
	if len(moved) > 0 {
		s.broadcastTabsLocked()
	}
}

// tabsUnderLocked returns the open files at rel or beneath it, in tab order.
func (s *Server) tabsUnderLocked(rel string) []*openFile {
	var out []*openFile
	for _, id := range s.tabs {
		f := s.byId[id]
		if f != nil && (f.path == rel || strings.HasPrefix(f.path, rel+"/")) {
			out = append(out, f)
		}
	}
	return out
}
