package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateSettingsSchema_SeedsDefaultRow(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got != defaultSettings() {
		t.Fatalf("expected freshly-created Settings row to match defaultSettings(), got %+v", got)
	}
}

func TestSaveSettings_PersistsToDB(t *testing.T) {
	db := newTestDB(t)
	s := defaultSettings()
	s.TabCloseIcon = "hidden"
	s.LinesPerResult = 9

	if err := db.SaveSettings(s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got, err := db.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got != s {
		t.Fatalf("expected saved settings to round-trip, got %+v want %+v", got, s)
	}
}

func TestValidateSettings_AcceptsDefaults(t *testing.T) {
	if err := validateSettings(defaultSettings()); err != nil {
		t.Fatalf("expected defaultSettings() to be valid, got %v", err)
	}
}

func TestValidateSettings_RejectsBadDurationFormat(t *testing.T) {
	s := defaultSettings()
	s.TrashTTL = "thirty days"
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for malformed duration, got nil")
	}
}

func TestValidateSettings_RejectsNonIncreasingTTLs(t *testing.T) {
	s := defaultSettings()
	s.MedTermTTL = s.ShortTermTTL // equal, not strictly increasing
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for non-increasing TTL tiers, got nil")
	}
}

func TestValidateSettings_RejectsNonIncreasingMinDelays(t *testing.T) {
	s := defaultSettings()
	s.LongTermMinDelay = "1s" // smaller than MedTermMinDelay
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for non-increasing MinDelay tiers, got nil")
	}
}

// Zero-valued durations are an accepted edge case (e.g. instant expiry), not rejected.
func TestValidateSettings_AllowsZeroDuration(t *testing.T) {
	s := defaultSettings()
	s.ShortTermTTL = "0s"
	if err := validateSettings(s); err != nil {
		t.Fatalf("expected zero duration to be valid, got %v", err)
	}
}

func TestValidateSettings_RejectsNonPositiveSearchFields(t *testing.T) {
	s := defaultSettings()
	s.LinesPerResult = 0
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for linesPerResult=0, got nil")
	}

	s = defaultSettings()
	s.MaxResultsPerFile = -1
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for negative maxResultsPerFile, got nil")
	}

	s = defaultSettings()
	s.MaxFiles = 0
	if err := validateSettings(s); err == nil {
		t.Fatalf("expected error for maxFiles=0, got nil")
	}
}

func TestSetSettingsCache_UpdatesRetentionVars(t *testing.T) {
	newTestDB(t) // NewDB already calls setSettingsCache(defaults); reset to a known state

	s := defaultSettings()
	s.TrashTTL = "45d"
	s.ShortTermTTL = "1m"
	if err := setSettingsCache(s); err != nil {
		t.Fatalf("setSettingsCache: %v", err)
	}

	if TrashTTL != 45*24*time.Hour {
		t.Fatalf("expected TrashTTL updated to 45d, got %v", TrashTTL)
	}
	if ShortTermTTL != time.Minute {
		t.Fatalf("expected ShortTermTTL updated to 1m, got %v", ShortTermTTL)
	}
	if GetSettingsCache() != s {
		t.Fatalf("expected GetSettingsCache to reflect the new settings")
	}
}

func TestHandleGetSettings_ReturnsCache(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	s.handleGetSettings(w, req)

	var got Settings
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != defaultSettings() {
		t.Fatalf("expected default settings, got %+v", got)
	}
}

func putSettings(t *testing.T, s *Server, body Settings) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.handleSaveSettings(w, req)
	return w
}

func TestHandleSaveSettings_RejectsInvalidWithoutPersisting(t *testing.T) {
	s := newTestServer(t)
	bad := defaultSettings()
	bad.TrashTTL = "not a duration"

	w := putSettings(t, s, bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if GetSettingsCache() != defaultSettings() {
		t.Fatalf("expected cache untouched by rejected save")
	}
	got, err := s.db.GetSettings()
	if err != nil || got != defaultSettings() {
		t.Fatalf("expected DB untouched by rejected save, got %+v err=%v", got, err)
	}
}

func TestHandleSaveSettings_PersistsAndAppliesImmediately(t *testing.T) {
	s := newTestServer(t)
	updated := defaultSettings()
	updated.TabCloseIcon = "hidden"
	updated.TrashTTL = "7d"

	w := putSettings(t, s, updated)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Settings
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp != updated {
		t.Fatalf("expected response to echo saved settings, got %+v", resp)
	}
	if GetSettingsCache() != updated {
		t.Fatalf("expected cache updated immediately after save")
	}
	dbGot, err := s.db.GetSettings()
	if err != nil || dbGot != updated {
		t.Fatalf("expected DB row updated, got %+v err=%v", dbGot, err)
	}
}

func TestHandleUpdateWrap_PersistsIndependentlyOfOtherFields(t *testing.T) {
	s := newTestServer(t)

	b, err := json.Marshal(map[string]bool{"wordWrap": false})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("PUT", "/api/settings/wordwrap", bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.handleUpdateWrap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := GetSettingsCache(); got.WordWrap != false {
		t.Fatalf("expected cache WordWrap=false, got %+v", got)
	}
	want := defaultSettings()
	want.WordWrap = false
	dbGot, err := s.db.GetSettings()
	if err != nil || dbGot != want {
		t.Fatalf("expected only WordWrap changed in DB, got %+v err=%v", dbGot, err)
	}
}

// Proves the live-reload wiring end-to-end: shrinking TrashTTL via the settings API
// changes what the very next FileVersioning run purges, with no restart involved.
func TestSettingsChange_AffectsNextFileVersioningRun(t *testing.T) {
	s := newTestServer(t)
	shrunk := defaultSettings()
	shrunk.TrashTTL = "1s"
	if w := putSettings(t, s, shrunk); w.Code != http.StatusOK {
		t.Fatalf("putSettings: %d: %s", w.Code, w.Body.String())
	}

	old := time.Now().Add(-2 * time.Second).UnixMilli()
	fileId := trashTestFile(t, s.db, "a.txt", "hello", old)

	runFileVersioningOnce(s.db, s.hub.Versions)

	if _, found, _ := s.db.GetFileTrash(fileId); found {
		t.Fatalf("expected trash row older than the newly-saved 1s TrashTTL to be purged")
	}
}
