package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// trashTestFile writes a real file, tracks it, then trashes it — TrashFile only
// inserts a FileTrash row when it actually wins the files-row delete, so DB-level
// trash tests need a real backing row rather than an arbitrary fileId string.
func trashTestFile(t *testing.T, db *DB, path, content string, dateDeleted int64) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(db.root, path), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := db.EnsureFileTracked(path)
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	if err := db.TrashFile(fileId, path, content, dateDeleted); err != nil {
		t.Fatalf("TrashFile: %v", err)
	}
	return fileId
}

func TestTrashFile_InsertsRowAndRemovesFilesRow(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	if err := s.db.TrashFile(f.FileId, f.Path, f.Content, time.Now().UnixMilli()); err != nil {
		t.Fatalf("TrashFile: %v", err)
	}

	if got, err := s.db.GetFile(f.FileId); err != nil || got != nil {
		t.Fatalf("expected files row removed, got %+v err=%v", got, err)
	}
	trashed, found, err := s.db.GetFileTrash(f.FileId)
	if err != nil || !found {
		t.Fatalf("expected trash row inserted, found=%v err=%v", found, err)
	}
	if trashed.Content != "hello" || trashed.Path != "a.txt" {
		t.Fatalf("expected trash row to capture path/content, got %+v", trashed)
	}
}

// Disk removal (os.Remove from an API handler) always races against the watcher's own
// fsnotify Remove handler reaching the same fileId — both end up calling TrashFile.
// Only the call that actually deletes the files row should insert a FileTrash row.
func TestTrashFile_ConcurrentCallsInsertOnlyOneTrashRow(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	if err := s.db.TrashFile(f.FileId, f.Path, f.Content, 1000); err != nil {
		t.Fatalf("first TrashFile: %v", err)
	}
	if err := s.db.TrashFile(f.FileId, f.Path, f.Content, 1000); err != nil {
		t.Fatalf("second (racing) TrashFile: %v", err)
	}

	list, err := s.db.GetTrashList()
	if err != nil {
		t.Fatalf("GetTrashList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 trash row despite two TrashFile calls, got %d: %+v", len(list), list)
	}
}

// GetTrashList derives LineCount/Length from the stored content/size at read time and
// never includes the content itself in its summary rows.
func TestGetTrashList_SortedNewestFirstWithDerivedFields(t *testing.T) {
	db := newTestDB(t)
	f1 := trashTestFile(t, db, "a.txt", "line1\nline2", 1000)
	f2 := trashTestFile(t, db, "b.txt", "x", 2000)

	list, err := db.GetTrashList()
	if err != nil {
		t.Fatalf("GetTrashList: %v", err)
	}
	if len(list) != 2 || list[0].FileId != f2 || list[1].FileId != f1 {
		t.Fatalf("expected newest-deleted-first order [%s,%s], got %+v", f2, f1, list)
	}
	if list[1].LineCount != 2 {
		t.Fatalf("expected lineCount 2 for 2-line content, got %+v", list[1])
	}
	if list[1].Length != len("line1\nline2") {
		t.Fatalf("expected length to match byte length, got %+v", list[1])
	}
}

func TestGetFileTrash_FoundAndNotFound(t *testing.T) {
	db := newTestDB(t)
	f1 := trashTestFile(t, db, "a.txt", "hello", 1000)

	got, found, err := db.GetFileTrash(f1)
	if err != nil || !found {
		t.Fatalf("expected found, err=%v", err)
	}
	if got.Content != "hello" || got.Path != "a.txt" || got.Size != len("hello") {
		t.Fatalf("unexpected trash record: %+v", got)
	}

	_, found, err = db.GetFileTrash("nonexistent")
	if err != nil || found {
		t.Fatalf("expected not-found for unknown fileId, found=%v err=%v", found, err)
	}
}

func TestDeleteFileTrash_RemovesOnlyThatRow(t *testing.T) {
	db := newTestDB(t)
	f1 := trashTestFile(t, db, "a.txt", "hello", 1000)
	f2 := trashTestFile(t, db, "b.txt", "world", 1000)

	if err := db.DeleteFileTrash(f1); err != nil {
		t.Fatalf("DeleteFileTrash: %v", err)
	}
	list, _ := db.GetTrashList()
	if len(list) != 1 || list[0].FileId != f2 {
		t.Fatalf("expected only %s to remain, got %+v", f2, list)
	}
}

func TestDeleteAllFileTrash_EmptiesEverything(t *testing.T) {
	db := newTestDB(t)
	trashTestFile(t, db, "a.txt", "hello", 1000)
	trashTestFile(t, db, "b.txt", "world", 1000)

	if err := db.DeleteAllFileTrash(); err != nil {
		t.Fatalf("DeleteAllFileTrash: %v", err)
	}
	list, _ := db.GetTrashList()
	if len(list) != 0 {
		t.Fatalf("expected trash empty, got %+v", list)
	}
}

// DeleteExpiredFileTrash should only remove rows older than TrashTTL, mirroring
// DeleteExpiredFileVersions' per-row age check.
func TestDeleteExpiredFileTrash_RespectsTrashTTL(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	old := now.Add(-TrashTTL - time.Minute).UnixMilli()
	recent := now.Add(-time.Minute).UnixMilli()
	expired := trashTestFile(t, db, "a.txt", "hello", old)
	fresh := trashTestFile(t, db, "b.txt", "world", recent)

	if err := db.DeleteExpiredFileTrash(now); err != nil {
		t.Fatalf("DeleteExpiredFileTrash: %v", err)
	}

	if _, found, _ := db.GetFileTrash(expired); found {
		t.Fatalf("expected expired trash row to be deleted")
	}
	if _, found, _ := db.GetFileTrash(fresh); !found {
		t.Fatalf("expected fresh trash row to survive")
	}
}

func deleteTestFile(t *testing.T, s *Server, fileId string) {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/api/files/"+fileId, nil)
	req.SetPathValue("id", fileId)
	w := httptest.NewRecorder()
	s.handleDeleteFile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("handleDeleteFile: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteFile_TrashesInsteadOfHardDelete(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	deleteTestFile(t, s, f.FileId)

	if got, err := s.db.GetFile(f.FileId); err != nil || got != nil {
		t.Fatalf("expected files row removed, got %+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected disk file removed, stat err=%v", err)
	}
	trashed, found, err := s.db.GetFileTrash(f.FileId)
	if err != nil || !found {
		t.Fatalf("expected trash row, found=%v err=%v", found, err)
	}
	if trashed.Content != "hello" {
		t.Fatalf("expected trash row to carry original content, got %+v", trashed)
	}
}

func TestHandleDeleteFolder_TrashesContainedFiles(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f1 := createTestFile(t, s, "sub/a.txt", "one")
	f2 := createTestFile(t, s, "sub/b.txt", "two")

	req := httptest.NewRequest("DELETE", "/api/folders", strings.NewReader(`{"path":"sub"}`))
	w := httptest.NewRecorder()
	s.handleDeleteFolder(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	for _, f := range []*DBFile{f1, f2} {
		trashed, found, err := s.db.GetFileTrash(f.FileId)
		if err != nil || !found {
			t.Fatalf("expected trash row for %s, found=%v err=%v", f.Path, found, err)
		}
		if trashed.Content != f.Content {
			t.Fatalf("expected trash row content to match original for %s, got %+v", f.Path, trashed)
		}
	}
	if _, err := os.Stat(filepath.Join(s.root, "sub")); !os.IsNotExist(err) {
		t.Fatalf("expected folder removed from disk, stat err=%v", err)
	}
}

func restoreTestFile(t *testing.T, s *Server, fileId string) map[string]string {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/trash/"+fileId+"/restore", nil)
	req.SetPathValue("id", fileId)
	w := httptest.NewRecorder()
	s.handleRestoreFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleRestoreFile: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	return resp
}

func TestHandleRestoreFile_ReusesFileIdAndClearsTrashRow(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")
	deleteTestFile(t, s, f.FileId)

	resp := restoreTestFile(t, s, f.FileId)

	if resp["fileId"] != f.FileId {
		t.Fatalf("expected restore to reuse original FileId, got %q", resp["fileId"])
	}
	if resp["path"] != "a.txt" {
		t.Fatalf("expected restore to original path, got %q", resp["path"])
	}
	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil {
		t.Fatalf("expected files row restored: %v", err)
	}
	if got.Content != "hello" {
		t.Fatalf("expected restored content, got %q", got.Content)
	}
	if got.VersionId == "" {
		t.Fatalf("expected a fresh VersionId to be minted")
	}
	if _, found, _ := s.db.GetFileTrash(f.FileId); found {
		t.Fatalf("expected trash row deleted after successful restore")
	}
	content, err := os.ReadFile(filepath.Join(s.root, "a.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("expected file rewritten to disk: %v %q", err, content)
	}
}

// Restoring onto an occupied path should rename exactly like Duplicate does, landing
// on "(2)" on the very first collision rather than "(1)".
func TestHandleRestoreFile_NameCollisionUsesNextDuplicateName(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")
	deleteTestFile(t, s, f.FileId)
	createTestFile(t, s, "a.txt", "occupied")

	resp := restoreTestFile(t, s, f.FileId)

	if resp["path"] != "a (2).txt" {
		t.Fatalf("expected collision to land on '(2)', got %q", resp["path"])
	}
	content, err := os.ReadFile(filepath.Join(s.root, "a (2).txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("expected restored content written under the renamed path: %v %q", err, content)
	}
}

func TestHandleRestoreFile_RecreatesMissingParentFolder(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := createTestFile(t, s, "sub/a.txt", "hello")
	deleteTestFile(t, s, f.FileId)

	if err := os.Remove(filepath.Join(s.root, "sub")); err != nil {
		t.Fatalf("remove now-empty folder: %v", err)
	}

	restoreTestFile(t, s, f.FileId)

	content, err := os.ReadFile(filepath.Join(s.root, "sub", "a.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("expected folder recreated and file restored: %v %q", err, content)
	}
}

// Restored files append at Max(OrderNum)+1 in their destination folder, the same rule
// used for any newly created file.
func TestHandleRestoreFile_OrderNumAppendedAtEnd(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")
	deleteTestFile(t, s, f.FileId)
	other := createTestFile(t, s, "b.txt", "other")

	restoreTestFile(t, s, f.FileId)

	restored, err := s.db.GetFile(f.FileId)
	if err != nil || restored == nil {
		t.Fatalf("GetFile restored: %v", err)
	}
	otherFile, err := s.db.GetFile(other.FileId)
	if err != nil || otherFile == nil {
		t.Fatalf("GetFile other: %v", err)
	}
	if restored.OrderNum <= otherFile.OrderNum {
		t.Fatalf("expected restored file appended after pre-existing file: restored=%d other=%d", restored.OrderNum, otherFile.OrderNum)
	}
}

// The write goes through the same RecentFileVersions seeding as any other edit, so a
// post-restore lookup by the fresh VersionId must succeed.
func TestHandleRestoreFile_SeedsRecentVersionStore(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")
	deleteTestFile(t, s, f.FileId)

	resp := restoreTestFile(t, s, f.FileId)

	content, found := s.hub.Versions.Lookup(f.FileId, resp["versionId"])
	if !found || content != "hello" {
		t.Fatalf("expected fresh version cached in RecentVersionStore, found=%v content=%q", found, content)
	}
}

func TestHandleListTrash_ReturnsSummariesWithoutContent(t *testing.T) {
	s := newTestServer(t)
	f1 := createTestFile(t, s, "a.txt", "line1\nline2")
	f2 := createTestFile(t, s, "b.txt", "x")
	deleteTestFile(t, s, f1.FileId)
	deleteTestFile(t, s, f2.FileId)

	req := httptest.NewRequest("GET", "/api/trash", nil)
	w := httptest.NewRecorder()
	s.handleListTrash(w, req)

	if !strings.Contains(w.Body.String(), `"lineCount"`) || strings.Contains(w.Body.String(), `"content"`) {
		t.Fatalf("expected summary fields without content, got %s", w.Body.String())
	}
	var list []FileTrashSummary
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %+v", list)
	}
	gotIds := map[string]bool{list[0].FileId: true, list[1].FileId: true}
	if !gotIds[f1.FileId] || !gotIds[f2.FileId] {
		t.Fatalf("expected both deleted files present, got %+v", list)
	}
}

func TestHandleGetTrashContent_ReturnsContentOrNotFound(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello world")
	deleteTestFile(t, s, f.FileId)

	req := httptest.NewRequest("GET", "/api/trash/"+f.FileId, nil)
	req.SetPathValue("id", f.FileId)
	w := httptest.NewRecorder()
	s.handleGetTrashContent(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["content"] != "hello world" {
		t.Fatalf("expected content, got %+v", resp)
	}

	req2 := httptest.NewRequest("GET", "/api/trash/nope", nil)
	req2.SetPathValue("id", "nope")
	w2 := httptest.NewRecorder()
	s.handleGetTrashContent(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown fileId, got %d", w2.Code)
	}
}

func TestHandleDeleteTrashItem_RemovesOnlyThatRow(t *testing.T) {
	s := newTestServer(t)
	f1 := createTestFile(t, s, "a.txt", "hello")
	f2 := createTestFile(t, s, "b.txt", "world")
	deleteTestFile(t, s, f1.FileId)
	deleteTestFile(t, s, f2.FileId)

	req := httptest.NewRequest("DELETE", "/api/trash/"+f1.FileId, nil)
	req.SetPathValue("id", f1.FileId)
	w := httptest.NewRecorder()
	s.handleDeleteTrashItem(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if _, found, _ := s.db.GetFileTrash(f1.FileId); found {
		t.Fatalf("expected f1 trash row deleted")
	}
	if _, found, _ := s.db.GetFileTrash(f2.FileId); !found {
		t.Fatalf("expected f2 trash row untouched")
	}
}

func TestHandleEmptyTrash_DeletesEveryRow(t *testing.T) {
	s := newTestServer(t)
	f1 := createTestFile(t, s, "a.txt", "hello")
	f2 := createTestFile(t, s, "b.txt", "world")
	deleteTestFile(t, s, f1.FileId)
	deleteTestFile(t, s, f2.FileId)

	req := httptest.NewRequest("DELETE", "/api/trash", nil)
	w := httptest.NewRecorder()
	s.handleEmptyTrash(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	list, _ := s.db.GetTrashList()
	if len(list) != 0 {
		t.Fatalf("expected trash empty, got %+v", list)
	}
}
