package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// DeleteExpiredFileVersions selects each row's TTL by its own Term, so a Term=1 row
// past ShortTermTTL is deleted while a Term=4 row at the same age survives. Uses
// distinct FileIds so the cumulative Term>=1 threshold on one file's surviving
// Term=4 row can't be mistaken for the other file's deleted Term=1 row.
func TestDeleteExpiredFileVersions_PerTermTTL(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	old := now.Add(-ShortTermTTL - time.Minute).UnixMilli() // past short TTL, well within very-long TTL

	if err := db.InsertFileVersion(FileVersion{FileId: "short", Path: "a.txt", Content: "c1", VersionId: "v1", Date: old, Term: 1}); err != nil {
		t.Fatalf("insert term1: %v", err)
	}
	if err := db.InsertFileVersion(FileVersion{FileId: "long", Path: "b.txt", Content: "c2", VersionId: "v2", Date: old, Term: 4}); err != nil {
		t.Fatalf("insert term4: %v", err)
	}

	if err := db.DeleteExpiredFileVersions(now); err != nil {
		t.Fatalf("DeleteExpiredFileVersions: %v", err)
	}

	dates, err := db.GetLastVersionDates([]string{"short", "long"})
	if err != nil {
		t.Fatalf("GetLastVersionDates: %v", err)
	}
	if _, ok := dates["short"]; ok {
		t.Fatalf("expected short-tier row to have expired and been deleted, got %+v", dates["short"])
	}
	if dates["long"][3] != old {
		t.Fatalf("expected Term=4 row to survive, got %+v", dates["long"])
	}
}

// GetLastVersionDates returns the MAX(Date) per cumulative term threshold across
// multiple FileIds in one query, treating Term as cumulative (a Term=3 row counts
// toward thresholds 1, 2, and 3).
func TestGetLastVersionDates_CumulativeAcrossFiles(t *testing.T) {
	db := newTestDB(t)
	t1 := time.Now().Add(-time.Hour).UnixMilli()
	t2 := time.Now().Add(-time.Minute).UnixMilli()

	if err := db.InsertFileVersion(FileVersion{FileId: "f1", Path: "a.txt", Content: "c1", VersionId: "v1", Date: t1, Term: 3}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertFileVersion(FileVersion{FileId: "f2", Path: "b.txt", Content: "c2", VersionId: "v2", Date: t2, Term: 1}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	dates, err := db.GetLastVersionDates([]string{"f1", "f2", "f3"})
	if err != nil {
		t.Fatalf("GetLastVersionDates: %v", err)
	}

	f1 := dates["f1"]
	if f1[0] != t1 || f1[1] != t1 || f1[2] != t1 || f1[3] != 0 {
		t.Fatalf("f1 cumulative thresholds wrong: %+v", f1)
	}
	f2 := dates["f2"]
	if f2[0] != t2 || f2[1] != 0 {
		t.Fatalf("f2 cumulative thresholds wrong: %+v", f2)
	}
	if _, ok := dates["f3"]; ok {
		t.Fatalf("expected no entry for fileId with zero rows")
	}
}

func TestGetFileVersionsByFileId(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UnixMilli()

	if err := db.InsertFileVersion(FileVersion{FileId: "f1", Path: "a.txt", Content: "c1", VersionId: "v1", Date: now, Term: 1}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertFileVersion(FileVersion{FileId: "f1", Path: "a.txt", Content: "c2", VersionId: "v2", Date: now, Term: 2}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertFileVersion(FileVersion{FileId: "other", Path: "b.txt", Content: "c3", VersionId: "v3", Date: now, Term: 1}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := db.GetFileVersionsByFileId("f1")
	if err != nil {
		t.Fatalf("GetFileVersionsByFileId: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows for f1, got %d: %+v", len(got), got)
	}

	none, err := db.GetFileVersionsByFileId("nonexistent")
	if err != nil {
		t.Fatalf("GetFileVersionsByFileId: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected 0 rows for unknown fileId, got %d", len(none))
	}
}

// buildFileVersionResponses tags persisted rows pending:false and preview rows
// pending:true, then orders the merged list newest-first by Date regardless of which
// source each entry came from.
func TestBuildFileVersionResponses_MergesTagsAndSortsNewestFirst(t *testing.T) {
	persisted := []FileVersion{
		{FileId: "f1", VersionId: "old", Date: 100, Term: 1},
		{FileId: "f1", VersionId: "newest", Date: 300, Term: 2},
	}
	preview := []FileVersion{
		{FileId: "f1", VersionId: "mid-preview", Date: 200, Term: 1},
	}

	out := buildFileVersionResponses(persisted, preview)
	if len(out) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out))
	}

	wantOrder := []string{"newest", "mid-preview", "old"}
	for i, versionId := range wantOrder {
		if out[i].VersionId != versionId {
			t.Fatalf("position %d: expected versionId %q, got %q (%+v)", i, versionId, out[i].VersionId, out)
		}
	}
	if out[1].Pending != true {
		t.Fatalf("expected preview entry to be Pending=true, got %+v", out[1])
	}
	if out[0].Pending || out[2].Pending {
		t.Fatalf("expected persisted entries to be Pending=false, got %+v", out)
	}
}

// The handler prepends currentSnapshotResponse rather than merging it into the
// sorted list, so it lands at element 0 regardless of its zero-value Date sentinel
// even when real past versions have far larger (newer) Date values.
func TestCurrentSnapshotResponse_PinnedFirstRegardlessOfDate(t *testing.T) {
	f := &DBFile{FileId: "f1", Path: "a.txt", Content: "now"}
	persisted := []FileVersion{{FileId: "f1", VersionId: "v1", Date: 999999, Term: 4}}

	out := append([]FileVersionResponse{currentSnapshotResponse(f)}, buildFileVersionResponses(persisted, nil)...)

	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	if !out[0].Current {
		t.Fatalf("expected element 0 to be the current snapshot, got %+v", out[0])
	}
	if out[0].Content != "now" {
		t.Fatalf("expected current content sourced from DBFile.Content, got %q", out[0].Content)
	}
	if out[1].Current {
		t.Fatalf("expected past version to have Current=false, got %+v", out[1])
	}
}

// Both inputs empty must still yield a non-nil, zero-length slice so the JSON
// response is `[]`, matching the /api/search convention of never returning null.
func TestBuildFileVersionResponses_EmptyInputsReturnNonNilSlice(t *testing.T) {
	out := buildFileVersionResponses(nil, nil)
	if out == nil {
		t.Fatalf("expected non-nil empty slice, got nil")
	}
	if len(out) != 0 {
		t.Fatalf("expected zero-length slice, got %d", len(out))
	}
}

func TestCurrentOrTrashedSnapshotResponse_PrefersLiveFileOverTrash(t *testing.T) {
	f := &DBFile{FileId: "f1", Path: "a.txt", Content: "live"}
	trash := &FileTrash{FileId: "f1", Path: "old.txt", Content: "trashed"}

	resp, found := currentOrTrashedSnapshotResponse(f, trash)
	if !found || !resp.Current || resp.Trashed {
		t.Fatalf("expected a live file to win over a trash row, got %+v found=%v", resp, found)
	}
	if resp.Content != "live" {
		t.Fatalf("expected live content, got %q", resp.Content)
	}
}

// A deleted file whose FileTrash row hasn't expired yet gets a "Trashed Content" entry
// instead of "Current Snapshot" — the closest thing to current a gone file still has.
func TestCurrentOrTrashedSnapshotResponse_FallsBackToTrash(t *testing.T) {
	trash := &FileTrash{FileId: "f1", Path: "old.txt", Content: "trashed"}

	resp, found := currentOrTrashedSnapshotResponse(nil, trash)
	if !found {
		t.Fatalf("expected found=true when a trash row exists")
	}
	if !resp.Current || !resp.Trashed {
		t.Fatalf("expected Current=true and Trashed=true, got %+v", resp)
	}
	if resp.Content != "trashed" || resp.Path != "old.txt" {
		t.Fatalf("expected content/path sourced from the trash row, got %+v", resp)
	}
}

// A file gone from both `files` and FileTrash (fully expired) has no current entry at
// all — callers fall back to showing only its past FileVersion rows, if any.
func TestCurrentOrTrashedSnapshotResponse_NeitherExistsReturnsNotFound(t *testing.T) {
	_, found := currentOrTrashedSnapshotResponse(nil, nil)
	if found {
		t.Fatalf("expected found=false when neither a live file nor a trash row exists")
	}
}

func TestHandleGetFileVersions_TrashedContentFallbackForOrphanedFile(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "a.txt", "hello")
	deleteTestFile(t, s, f.FileId)

	req := httptest.NewRequest("GET", "/api/files/"+f.FileId+"/versions", nil)
	req.SetPathValue("id", f.FileId)
	w := httptest.NewRecorder()
	s.handleGetFileVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out []FileVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || !out[0].Current || !out[0].Trashed {
		t.Fatalf("expected a single Trashed Content entry, got %+v", out)
	}
	if out[0].Content != "hello" {
		t.Fatalf("expected content sourced from the trash row, got %+v", out[0])
	}
}

// Once a file's trash row has also been purged (e.g. by TTL), there's nothing current
// to show, but its past FileVersion rows should still be listed rather than 404ing.
func TestHandleGetFileVersions_OmitsCurrentEntryWhenFullyGoneButKeepsHistory(t *testing.T) {
	s := newTestServer(t)
	if err := s.db.InsertFileVersion(FileVersion{FileId: "gone", Path: "old.txt", Content: "past", VersionId: "v1", Date: 1, Term: 1}); err != nil {
		t.Fatalf("InsertFileVersion: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/files/gone/versions", nil)
	req.SetPathValue("id", "gone")
	w := httptest.NewRecorder()
	s.handleGetFileVersions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out []FileVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].Current {
		t.Fatalf("expected only the past version with no pinned current entry, got %+v", out)
	}
	if out[0].VersionId != "v1" {
		t.Fatalf("expected the past version preserved, got %+v", out[0])
	}
}

// A FileId with no live file, no trash row, and no version history at all has truly
// never existed (or has fully aged out everywhere) — still 404s, same as before.
func TestHandleGetFileVersions_404WhenNothingExistsAtAll(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/files/nonexistent/versions", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	s.handleGetFileVersions(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
