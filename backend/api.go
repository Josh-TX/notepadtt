package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) {
	json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleGetTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, BuildTree(s.rootAbs))
}

// ---- tabs ----

func (s *Server) handleGetTabs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := ""
	if s.byId[s.lastActive] != nil {
		active = s.lastActive
	}
	writeJSON(w, map[string]any{"tabs": s.tabsInfoLocked(), "activeFileId": active})
}

func (s *Server) handleOpenTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	decodeBody(r, &body)
	if !validRel(body.Path) {
		http.Error(w, "invalid path", 400)
		return
	}
	info, err := os.Stat(s.abs(body.Path))
	if err != nil || info.IsDir() {
		http.Error(w, "not a file", 404)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, existed := s.byPath[body.Path]
	f := s.openTabLocked(body.Path)
	if !existed {
		s.broadcastTabsLocked()
	}
	writeJSON(w, s.tabInfoLocked(f))
}

func (s *Server) handleCloseTab(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.byId[id]; f != nil {
		s.flushLocked(f)
	}
	if s.closeTabLocked(id) >= 0 {
		s.broadcastTabsLocked()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderTabs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileIds []string `json:"fileIds"`
	}
	decodeBody(r, &body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(body.FileIds) != len(s.tabs) {
		http.Error(w, "tab list out of date", 409)
		return
	}
	seen := map[string]bool{}
	for _, id := range body.FileIds {
		if s.byId[id] == nil || seen[id] {
			http.Error(w, "tab list out of date", 409)
			return
		}
		seen[id] = true
	}
	s.tabs = body.FileIds
	s.broadcastTabsLocked()
	w.WriteHeader(http.StatusNoContent)
}

// ---- file content ----

type fileResponse struct {
	FileId    string `json:"fileId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	VersionId string `json:"versionId"`
	TooLarge  bool   `json:"tooLarge,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Size      int64  `json:"size"`
}

// isBinaryFile reports whether the first chunk of the file contains a NUL byte.
func isBinaryFile(abs string) bool {
	f, err := os.Open(abs)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// handleGetFile returns a tab's content and (when ?cid= is given) subscribes that
// connection to it. Large files require ?force=1; binary files never return content.
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cid := r.URL.Query().Get("cid")
	force := r.URL.Query().Get("force") == "1"

	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.byId[id]
	if f == nil {
		http.NotFound(w, r)
		return
	}
	abs := s.abs(f.path)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if cid != "" {
		s.lastActive = id
	}
	setSub := func(fileId string) {
		if cid != "" {
			s.maybeUnloadLocked(s.hub.SetSubscription(cid, fileId))
		}
	}
	resp := fileResponse{FileId: id, Path: f.path, Size: info.Size()}

	if isBinaryFile(abs) {
		setSub("")
		resp.Binary = true
		writeJSON(w, resp)
		return
	}
	if info.Size() > maxDisplayBytes && !force {
		setSub("")
		resp.TooLarge = true
		writeJSON(w, resp)
		return
	}
	if !f.loaded {
		if err := s.loadLocked(f); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	setSub(id)
	resp.Content = f.content
	resp.VersionId = f.versionId
	writeJSON(w, resp)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if !validRel(rel) {
		http.Error(w, "invalid path", 400)
		return
	}
	abs := s.abs(rel)
	s.flushPath(rel)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(rel)))
	http.ServeFile(w, r, abs)
}

// ---- create / duplicate ----

func (s *Server) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPath string `json:"parentPath"`
	}
	decodeBody(r, &body)
	if body.ParentPath != "" && !validRel(body.ParentPath) {
		http.Error(w, "invalid path", 400)
		return
	}
	if info, err := os.Stat(s.abs(body.ParentPath)); err != nil || !info.IsDir() {
		http.Error(w, "parent folder not found", 404)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for n := 1; ; n++ {
		name := fmt.Sprintf("new %d", n)
		rel := joinRel(body.ParentPath, name)
		file, err := os.OpenFile(s.abs(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		file.WriteString("\n\n\n\n")
		file.Close()
		f := s.openTabLocked(rel)
		s.broadcastTabsLocked()
		s.scheduleTree()
		writeJSON(w, s.tabInfoLocked(f))
		return
	}
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPath string `json:"parentPath"`
		Name       string `json:"name"`
	}
	decodeBody(r, &body)
	if !validName(body.Name) || (body.ParentPath != "" && !validRel(body.ParentPath)) {
		http.Error(w, "invalid name", 400)
		return
	}
	if err := os.Mkdir(s.abs(joinRel(body.ParentPath, body.Name)), 0755); err != nil {
		code := 500
		if os.IsExist(err) {
			code = 409
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.scheduleTree()
	w.WriteHeader(http.StatusNoContent)
}

var dupSuffixRe = regexp.MustCompile(` \(\d+\)$`)

// nextDuplicateName returns "foo (2).txt" style names, incrementing until unused.
func (s *Server) nextDuplicateName(folder, base string) string {
	ext := filepath.Ext(base)
	stem := dupSuffixRe.ReplaceAllString(strings.TrimSuffix(base, ext), "")
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, err := os.Lstat(s.abs(joinRel(folder, candidate))); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *Server) handleDuplicate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	decodeBody(r, &body)
	if !validRel(body.Path) {
		http.Error(w, "invalid path", 400)
		return
	}
	s.flushPath(body.Path)
	src, err := os.Open(s.abs(body.Path))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer src.Close()
	if info, err := src.Stat(); err != nil || info.IsDir() {
		http.Error(w, "not a file", 400)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rel := joinRel(parentOf(body.Path), s.nextDuplicateName(parentOf(body.Path), path.Base(body.Path)))
	dst, err := os.OpenFile(s.abs(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_, err = io.Copy(dst, src)
	dst.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	f := s.openTabLocked(rel)
	s.broadcastTabsLocked()
	s.scheduleTree()
	writeJSON(w, s.tabInfoLocked(f))
}

func joinRel(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}

// ---- rename / move ----

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	decodeBody(r, &body)
	if !validRel(body.Path) || !validName(body.Name) {
		http.Error(w, "invalid name", 400)
		return
	}
	s.renamePath(w, body.Path, joinRel(parentOf(body.Path), body.Name))
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		NewPath string `json:"newPath"`
	}
	decodeBody(r, &body)
	if !validRel(body.Path) || !validRel(body.NewPath) {
		http.Error(w, "invalid path", 400)
		return
	}
	if body.NewPath == body.Path || strings.HasPrefix(body.NewPath, body.Path+"/") {
		http.Error(w, "cannot move into itself", 400)
		return
	}
	s.renamePath(w, body.Path, body.NewPath)
}

// renamePath moves a file or folder on disk and re-points any open tabs at it.
func (s *Server) renamePath(w http.ResponseWriter, oldRel, newRel string) {
	if oldRel == newRel {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(s.abs(oldRel)); err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if _, err := os.Lstat(s.abs(newRel)); err == nil {
		http.Error(w, "already exists", 409)
		return
	}
	for _, f := range s.tabsUnderLocked(oldRel) {
		s.flushLocked(f)
	}
	if err := os.Rename(s.abs(oldRel), s.abs(newRel)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.repathLocked(oldRel, newRel)
	s.scheduleTree()
	w.WriteHeader(http.StatusNoContent)
}
