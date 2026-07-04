package backend

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) broadcastTree(reason string) {
	files, _ := s.db.GetAllFilesOnDisk()
	s.hub.BroadcastFS(BuildTree(files, s.root), reason)
}

func (s *Server) handleGetFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.db.GetAllFilesOnDisk()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tree := BuildTree(files, s.root)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	cid := r.URL.Query().Get("cid")

	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	s.db.UpdateLastOpened(fileId)
	if cid != "" {
		s.hub.SetSubscription(cid, fileId)
	}
	log.Printf("[getFile] fileId=%s versionId=%q cid=%q", fileId, f.VersionId, cid)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"fileId":    f.FileId,
		"path":      f.Path,
		"content":   f.Content,
		"versionId": f.VersionId,
	})
}

// handleGetFileVersions returns the file's current content plus every known past
// version, newest-first. Element 0 is normally a synthetic current:true entry sourced
// from the file's live DB content, so callers needing the "current snapshot" don't
// need a second request — but a History search result can deep-link here for a FileId
// whose live file no longer exists, in which case element 0 instead sources from that
// FileId's FileTrash row (current:true, trashed:true) if one exists, or is omitted
// entirely if not (see currentOrTrashedSnapshotResponse). 404 is reserved for a FileId
// with no live file, no trash row, and no version history at all — i.e. it never
// existed, or has fully aged out everywhere. The rest are persisted FileVersions rows
// (pending:false) plus on-demand previews of entries in RecentFileVersions that would
// be promoted the next time the FileVersioning process runs (pending:true, nothing
// written to the DB). Both sources share assignTermsForFile (retention.go) so they can
// never disagree about what's eligible.
func (s *Server) handleGetFileVersions(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	f, err := s.db.GetFile(fileId)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	persisted, err := s.db.GetFileVersionsByFileId(fileId)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	lastDates, err := s.db.GetLastVersionDates([]string{fileId})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	last := lastDates[fileId]
	preview := assignTermsForFile(s.hub.Versions.EntriesForFile(fileId), &last)
	out := buildFileVersionResponses(persisted, preview)

	var trash *FileTrash
	if f == nil {
		if t, found, err := s.db.GetFileTrash(fileId); err != nil {
			http.Error(w, err.Error(), 500)
			return
		} else if found {
			trash = &t
		}
	}

	current, hasCurrent := currentOrTrashedSnapshotResponse(f, trash)
	if hasCurrent {
		out = append([]FileVersionResponse{current}, out...)
	} else if len(out) == 0 {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPath string `json:"parentPath"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	name := fmt.Sprintf("new %d", s.db.NextNewN(body.ParentPath))
	var relPath string
	if body.ParentPath == "" {
		relPath = name
	} else {
		relPath = body.ParentPath + "/" + name
	}

	diskPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	s.hub.RecentlyCreated.Mark(relPath)
	if err := os.WriteFile(diskPath, []byte("\n\n\n\n"), 0644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// EnsureFileTracked assigns Max(OrderNum)+1 within the folder.
	fileId, err := s.db.EnsureFileTracked(relPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.Error(w, "failed to load created file", 500)
		return
	}
	s.broadcastTree("API: file created")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"fileId":    f.FileId,
		"path":      f.Path,
		"content":   f.Content,
		"versionId": f.VersionId,
	})
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}

	dir := filepath.Dir(f.Path)
	if dir == "." {
		dir = ""
	}
	var newRelPath string
	if dir == "" {
		newRelPath = body.Name
	} else {
		newRelPath = dir + "/" + body.Name
	}

	oldDisk := filepath.Join(s.root, filepath.FromSlash(f.Path))
	newDisk := filepath.Join(s.root, filepath.FromSlash(newRelPath))
	if err := os.Rename(oldDisk, newDisk); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.db.UpdatePath(f.Path, newRelPath)
	s.broadcastTree("API: file renamed")
	w.WriteHeader(http.StatusNoContent)
}

// handleMoveFile relocates a file to a new full path (folder + filename), similar to
// handleRenameFile but accepting the complete destination path instead of just a new
// name. Returns 409 if something already occupies the destination — the MoveFileModal
// does client-side conflict detection so this is a safety net, not the primary UX.
// Creates any missing parent directories so users can move a file into a new folder
// by typing a novel path.
func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	var body struct {
		Path string `json:"path"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}

	newRelPath := body.Path
	newDisk := s.jailPath(newRelPath)
	if newDisk == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if newRelPath != f.Path {
		if _, err := os.Stat(newDisk); err == nil {
			http.Error(w, "a file or folder already exists at the destination", http.StatusConflict)
			return
		}

		destFolder := folderOf(newRelPath)
		if destFolder != "" {
			destDisk := filepath.Join(s.root, filepath.FromSlash(destFolder))
			if err := os.MkdirAll(destDisk, 0755); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		oldDisk := filepath.Join(s.root, filepath.FromSlash(f.Path))
		if err := os.Rename(oldDisk, newDisk); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		oldFolder := folderOf(f.Path)
		destMaxOrder := s.db.GetMaxOrderNumInFolder(destFolder)
		s.db.UpdatePath(f.Path, newRelPath)
		s.db.CompactOrderNums(oldFolder)
		s.db.SetOrderNum(fileId, destMaxOrder+1)
		s.broadcastTree("API: file moved")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"fileId":    fileId,
		"path":      newRelPath,
		"content":   f.Content,
		"versionId": f.VersionId,
	})
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	folder := folderOf(f.Path)
	diskPath := filepath.Join(s.root, filepath.FromSlash(f.Path))
	os.Remove(diskPath)
	s.db.TrashFile(f.FileId, f.Path, f.Content, time.Now().UnixMilli())
	s.db.CompactOrderNums(folder)
	s.broadcastTree("API: file deleted")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDuplicateFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}

	folder := folderOf(f.Path)
	baseName := filepath.Base(f.Path)
	newName := s.db.NextDuplicateName(folder, baseName)
	var newRelPath string
	if folder == "" {
		newRelPath = newName
	} else {
		newRelPath = folder + "/" + newName
	}

	diskPath := filepath.Join(s.root, filepath.FromSlash(newRelPath))
	s.hub.RecentlyCreated.Mark(newRelPath)
	if err := os.WriteFile(diskPath, []byte(f.Content), 0644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Insert duplicate immediately after the original in order.
	insertAt := f.OrderNum + 1
	s.db.ShiftOrderNumsUp(folder, insertAt)
	newFileId, err := s.db.InsertFileWithOrder(newRelPath, f.Content, insertAt)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.broadcastTree("API: file duplicated")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"fileId": newFileId})
}

func (s *Server) handleReorderFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	var body struct {
		TargetIndex int `json:"targetIndex"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}

	folder := folderOf(f.Path)
	files, err := s.db.GetFilesInFolderSorted(folder)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Remove the file from its current position.
	remaining := make([]DBFile, 0, len(files)-1)
	for _, file := range files {
		if file.FileId != fileId {
			remaining = append(remaining, file)
		}
	}

	// Clamp targetIndex and build the new ordered slice.
	idx := body.TargetIndex
	if idx < 0 {
		idx = 0
	}
	if idx > len(remaining) {
		idx = len(remaining)
	}
	ordered := make([]DBFile, 0, len(files))
	ordered = append(ordered, remaining[:idx]...)
	ordered = append(ordered, *f)
	ordered = append(ordered, remaining[idx:]...)

	for i, file := range ordered {
		s.db.SetOrderNum(file.FileId, i)
	}

	s.broadcastTree("API: file reordered")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentPath string `json:"parentPath"`
		Name       string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	var relPath string
	if body.ParentPath == "" {
		relPath = body.Name
	} else {
		relPath = body.ParentPath + "/" + body.Name
	}
	diskPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(diskPath, 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.broadcastTree("API: folder created")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	dir := filepath.Dir(body.Path)
	if dir == "." {
		dir = ""
	}
	var newRelPath string
	if dir == "" {
		newRelPath = body.Name
	} else {
		newRelPath = dir + "/" + body.Name
	}

	oldDisk := filepath.Join(s.root, filepath.FromSlash(body.Path))
	newDisk := filepath.Join(s.root, filepath.FromSlash(newRelPath))
	if err := os.Rename(oldDisk, newDisk); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.db.UpdateFolderPath(body.Path, newRelPath)
	s.broadcastTree("API: folder renamed")
	w.WriteHeader(http.StatusNoContent)
}

// handleMoveFolder relocates a folder to a new full path, similar to handleRenameFolder
// but accepting the complete destination path instead of just a new name. Unlike
// move-file, conflicts and cyclical moves (into the folder's own subtree) are rejected
// outright rather than auto-renamed — the FileTree drag UI already prevents both
// client-side, this is the defensive server-side backstop.
func (s *Server) handleMoveFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		NewPath string `json:"newPath"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.NewPath == body.Path {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.HasPrefix(body.NewPath, body.Path+"/") {
		http.Error(w, "cannot move a folder into itself or a descendant", http.StatusBadRequest)
		return
	}
	newDisk := s.jailPath(body.NewPath)
	if newDisk == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(newDisk); err == nil {
		http.Error(w, "a file or folder already exists at the destination", http.StatusConflict)
		return
	}

	if err := os.MkdirAll(filepath.Dir(newDisk), 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	oldDisk := filepath.Join(s.root, filepath.FromSlash(body.Path))
	if err := os.Rename(oldDisk, newDisk); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.db.UpdateFolderPath(body.Path, body.NewPath)
	s.broadcastTree("API: folder moved")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	force := r.URL.Query().Get("force") == "true"
	if !force {
		untracked, symlinks, err := s.countUntrackedFilesInFolder(body.Path)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if untracked > 0 || symlinks > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]int{"untrackedCount": untracked, "symlinkCount": symlinks})
			return
		}
	}

	files, err := s.db.GetFilesInFolderRecursive(body.Path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	diskPath := filepath.Join(s.root, filepath.FromSlash(body.Path))
	if err := os.RemoveAll(diskPath); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now().UnixMilli()
	for _, f := range files {
		s.db.TrashFile(f.FileId, f.Path, f.Content, now)
	}
	s.broadcastTree("API: folder deleted")
	w.WriteHeader(http.StatusNoContent)
}

// countUntrackedFilesInFolder reports, for a folder about to be deleted, how
// many real non-text files would be permanently lost (untracked) and how many
// symlinks would merely be unlinked. Symlinks are counted separately because
// deleting a folder never follows them - os.RemoveAll unlinks a symlink entry
// without touching whatever it points to - so their target content is never
// actually at risk, unlike genuine untracked files.
func (s *Server) countUntrackedFilesInFolder(folderRelPath string) (untracked int, symlinks int, err error) {
	diskPath := filepath.Join(s.root, filepath.FromSlash(folderRelPath))
	err = filepath.Walk(diskPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			symlinks++
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return nil
		}
		if !s.db.IsAllowedPath(filepath.ToSlash(relPath)) {
			untracked++
		}
		return nil
	})
	return
}

// handleListTrash returns every trashed file as a lightweight summary (no content),
// for the Trash Modal's sidebar list, most-recently-deleted first.
func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.GetTrashList()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handleGetTrashContent fetches one trashed file's content on demand, for the Trash
// Modal's read-only content pane.
func (s *Server) handleGetTrashContent(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	t, found, err := s.db.GetFileTrash(fileId)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"content": t.Content})
}

// handleRestoreFile restores a trashed file to its original path, reusing the
// original FileId so its FileVersion history reattaches immediately. If something
// already occupies the original path, it picks a new name the same way Duplicate
// does. Shared by the delete toast's UNDO action and the Trash Modal's restore button.
func (s *Server) handleRestoreFile(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	t, found, err := s.db.GetFileTrash(fileId)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	folder := folderOf(t.Path)
	relPath := t.Path
	if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(relPath))); err == nil {
		newName := s.db.NextDuplicateName(folder, filepath.Base(t.Path))
		if folder == "" {
			relPath = newName
		} else {
			relPath = folder + "/" + newName
		}
	}

	folderDisk := filepath.Join(s.root, filepath.FromSlash(folder))
	if err := os.MkdirAll(folderDisk, 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	diskPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	s.hub.RecentlyCreated.Mark(relPath)
	if err := os.WriteFile(diskPath, []byte(t.Content), 0644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	orderNum := s.db.GetMaxOrderNumInFolder(folder) + 1
	versionId, err := s.db.InsertFileWithId(fileId, relPath, t.Content, orderNum)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Seeds RecentFileVersions with this write like any other edit, so the normal
	// versioning pipeline (and any immediate post-restore conflict resolution) needs
	// no special-casing for restores.
	s.hub.Versions.Add(fileId, versionId, relPath, t.Content)
	s.db.DeleteFileTrash(fileId)

	s.broadcastTree("API: file restored")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"fileId":    fileId,
		"path":      relPath,
		"content":   t.Content,
		"versionId": versionId,
	})
}

// handleDeleteTrashItem permanently deletes one FileTrash row (the Trash Modal's
// "Delete Forever" action) with no confirmation, since it's a single low-stakes item
// already in the trash.
func (s *Server) handleDeleteTrashItem(w http.ResponseWriter, r *http.Request) {
	fileId := r.PathValue("id")
	if err := s.db.DeleteFileTrash(fileId); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEmptyTrash permanently deletes every FileTrash row (the Trash Modal's
// "Empty Trash" action).
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteAllFileTrash(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSearch reads the per-search source toggles (History/Trash, set by SearchModal's
// live checkboxes) from query params, and the result-shaping knobs (LinesPerResult,
// MaxResultsPerFile, MaxFiles) from the Settings cache — those three aren't exposed as
// SearchModal controls, only configurable via the Settings modal.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	terms := strings.Fields(r.URL.Query().Get("q"))
	settings := GetSettingsCache()
	opts := SearchOptions{
		IncludeHistory:    r.URL.Query().Get("history") == "true",
		IncludeTrash:      r.URL.Query().Get("trash") == "true",
		LinesPerResult:    settings.LinesPerResult,
		MaxResultsPerFile: settings.MaxResultsPerFile,
		MaxFiles:          settings.MaxFiles,
	}
	results, err := s.db.SearchFiles(terms, opts)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if results == nil {
		results = []SearchResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleGetSettings returns the in-memory settings cache, populated at startup and
// refreshed on every successful save (see setSettingsCache).
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetSettingsCache())
}

// handleSaveSettings replaces the entire Settings row atomically — the modal's single
// Save button always submits every field, so a validation failure rejects the whole
// request rather than applying a subset.
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body Settings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.EditorFontSize = clamp(body.EditorFontSize, 10, 24)
	if err := validateSettings(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.db.SaveSettings(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := setSettingsCache(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

// handleUpdateWrap persists just the WordWrap field, independent of the Settings
// modal's full-form save — backs the Footer's "Wrap" toggle button.
func (s *Server) handleUpdateWrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WordWrap bool `json:"wordWrap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.db.UpdateWordWrap(body.WordWrap); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	setWordWrapCache(body.WordWrap)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

// handleUpdateSidebarWidth persists just the SidebarWidth field, independent of the
// Settings modal's full-form save — backs the Sidebar's drag-to-resize handle. Out-of-range
// values are clamped to [100,600] rather than rejected.
func (s *Server) handleUpdateSidebarWidth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SidebarWidth int `json:"sidebarWidth"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.SidebarWidth = clamp(body.SidebarWidth, 100, 600)
	if err := s.db.UpdateSidebarWidth(body.SidebarWidth); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	setSidebarWidthCache(body.SidebarWidth)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

// handleUpdateDesktopSidebarOpen persists just the DesktopSidebarOpen field, independent
// of the Settings modal's full-form save — backs the Navbar's sidebar toggle and the
// Sidebar's collapse button, but only while the sidebar is in push-layout (desktop) mode.
func (s *Server) handleUpdateDesktopSidebarOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DesktopSidebarOpen bool `json:"desktopSidebarOpen"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.db.UpdateDesktopSidebarOpen(body.DesktopSidebarOpen); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	setDesktopSidebarOpenCache(body.DesktopSidebarOpen)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

// jailPath verifies relPath stays within root. Returns absolute path or "".
func (s *Server) jailPath(relPath string) string {
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return ""
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(relPath))
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return ""
	}
	return abs
}
