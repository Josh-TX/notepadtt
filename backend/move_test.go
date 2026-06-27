package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func moveFileRequest(t *testing.T, s *Server, fileId, path string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"path": path})
	req := httptest.NewRequest("PUT", "/api/files/"+fileId+"/move", bytes.NewReader(body))
	req.SetPathValue("id", fileId)
	w := httptest.NewRecorder()
	s.handleMoveFile(w, req)
	return w
}

func moveFolderRequest(t *testing.T, s *Server, path, newPath string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"path": path, "newPath": newPath})
	req := httptest.NewRequest("PUT", "/api/folders/move", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMoveFolder(w, req)
	return w
}

// jailPath must resolve s.root to an absolute path before prefix-matching — joining a
// relative root (e.g. "." as used by main.go's default -d flag) with a sub-path strips
// the relative prefix, so comparing against the un-resolved root always failed the
// prefix check and rejected every legitimate path as "invalid".
func TestJailPath_HandlesRelativeRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	rootAbs, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd after chdir: %v", err)
	}

	s := &Server{root: "."}
	got := s.jailPath("specs/GLOSSARY.md")
	want := filepath.Join(rootAbs, "specs", "GLOSSARY.md")
	if got != want {
		t.Fatalf("jailPath(%q) = %q, want %q", "specs/GLOSSARY.md", got, want)
	}
	if s.jailPath("../escape.txt") != "" {
		t.Fatalf("expected escape attempt to be rejected")
	}
}

func TestHandleMoveFile_MovesToNewFolder(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "dest"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := createTestFile(t, s, "a.txt", "hello")

	w := moveFileRequest(t, s, f.FileId, "dest/a.txt")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["path"] != "dest/a.txt" || resp["content"] != "hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil || got.Path != "dest/a.txt" {
		t.Fatalf("expected DB path updated to dest/a.txt, got %+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "dest", "a.txt")); err != nil {
		t.Fatalf("expected file on disk at new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file gone from old location, stat err=%v", err)
	}
}

// A name conflict at the destination should return 409 — the MoveFileModal does
// client-side conflict detection and disables the Move button, so this is the
// server-side backstop.
func TestHandleMoveFile_NameConflictReturns409(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "dest"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := createTestFile(t, s, "a.txt", "hello")
	createTestFile(t, s, "dest/a.txt", "occupied")

	w := moveFileRequest(t, s, f.FileId, "dest/a.txt")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	// Original file must remain at its source path
	if _, err := os.Stat(filepath.Join(s.root, "a.txt")); err != nil {
		t.Fatalf("expected source file to remain: %v", err)
	}
}

// Moving a file appends it at Max(OrderNum)+1 in the destination folder, and compacts
// the source folder's remaining OrderNums back to 0,1,2…, mirroring delete's behavior.
func TestHandleMoveFile_OrderNumAppendedAndSourceCompacted(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "dest"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	a := createTestFile(t, s, "a.txt", "1")
	b := createTestFile(t, s, "b.txt", "2")
	c := createTestFile(t, s, "c.txt", "3")
	destFile := createTestFile(t, s, "dest/x.txt", "x")

	moveFileRequest(t, s, b.FileId, "dest/b.txt")

	moved, _ := s.db.GetFile(b.FileId)
	destExisting, _ := s.db.GetFile(destFile.FileId)
	if moved.OrderNum <= destExisting.OrderNum {
		t.Fatalf("expected moved file appended after existing dest file: moved=%d dest=%d", moved.OrderNum, destExisting.OrderNum)
	}

	gotA, _ := s.db.GetFile(a.FileId)
	gotC, _ := s.db.GetFile(c.FileId)
	seen := map[int]bool{gotA.OrderNum: true, gotC.OrderNum: true}
	if len(seen) != 2 || !seen[0] || !seen[1] {
		t.Fatalf("expected source folder compacted to 0,1, got a=%d c=%d", gotA.OrderNum, gotC.OrderNum)
	}
}

// Dropping a file back onto its own current path (e.g. its own parent folder) should
// be a harmless no-op rather than erroring or renaming.
func TestHandleMoveFile_SamePathIsNoOp(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	w := moveFileRequest(t, s, f.FileId, "a.txt")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil || got.Path != "a.txt" || got.OrderNum != f.OrderNum {
		t.Fatalf("expected no change, got %+v err=%v", got, err)
	}
}

func TestHandleMoveFile_PathEscapingRootRejected(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	w := moveFileRequest(t, s, f.FileId, "../escape.txt")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// Moving a file to a path whose parent folder doesn't exist yet should create it
// automatically, so users can move into a new folder by typing a novel path.
func TestHandleMoveFile_CreatesParentFolderIfMissing(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")

	w := moveFileRequest(t, s, f.FileId, "newdir/a.txt")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.root, "newdir", "a.txt")); err != nil {
		t.Fatalf("expected file at new path: %v", err)
	}
}

// Moving a folder to a path whose parent doesn't exist should create parent dirs first.
func TestHandleMoveFolder_CreatesParentFolderIfMissing(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createTestFile(t, s, "sub/a.txt", "hello")

	w := moveFolderRequest(t, s, "sub", "newparent/sub")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.root, "newparent", "sub", "a.txt")); err != nil {
		t.Fatalf("expected folder relocated under new parent: %v", err)
	}
}

func TestHandleMoveFolder_MovesFolderAndContainedFiles(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.root, "dest"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := createTestFile(t, s, "sub/a.txt", "hello")

	w := moveFolderRequest(t, s, "sub", "dest/sub")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil || got.Path != "dest/sub/a.txt" {
		t.Fatalf("expected contained file's path updated, got %+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "dest", "sub", "a.txt")); err != nil {
		t.Fatalf("expected folder relocated on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "sub")); !os.IsNotExist(err) {
		t.Fatalf("expected old folder gone, stat err=%v", err)
	}
}

func TestHandleMoveFolder_RejectsMoveIntoOwnDescendant(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "A", "B"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := moveFolderRequest(t, s, "A", "A/B/A2")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.root, "A")); err != nil {
		t.Fatalf("expected folder untouched after rejected move: %v", err)
	}
}

func TestHandleMoveFolder_RejectsNameConflict(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.root, "dest", "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := moveFolderRequest(t, s, "sub", "dest/sub")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.root, "sub")); err != nil {
		t.Fatalf("expected source folder untouched after rejected move: %v", err)
	}
}

func TestHandleMoveFolder_SamePathIsNoOp(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := moveFolderRequest(t, s, "sub", "sub")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMoveFolder_PathEscapingRootRejected(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(filepath.Join(s.root, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w := moveFolderRequest(t, s, "sub", "../escape")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
