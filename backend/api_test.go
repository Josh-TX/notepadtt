package backend

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A tracked file whose on-disk size grows past MaxFileSizeKB without a rescan yet having
// caught up (e.g. edited directly on disk moments ago) should still 400 rather than serve
// stale/never-loaded content — a defense-in-depth check alongside the scan/watcher purge.
func TestHandleGetFile_RejectsOversizedFile(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "big.txt", "small for now")

	settings := defaultSettings()
	settings.MaxFileSizeKB = 1
	if err := setSettingsCache(settings); err != nil {
		t.Fatalf("setSettingsCache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "big.txt"), make([]byte, 2000), 0644); err != nil {
		t.Fatalf("write big.txt: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/files/"+f.FileId, nil)
	req.SetPathValue("id", f.FileId)
	w := httptest.NewRecorder()
	s.handleGetFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetFile_ServesFileWithinSizeLimit(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "small.txt", "hello")

	req := httptest.NewRequest("GET", "/api/files/"+f.FileId, nil)
	req.SetPathValue("id", f.FileId)
	w := httptest.NewRecorder()
	s.handleGetFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
