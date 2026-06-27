package backend

import (
	"testing"
	"time"
)

// EntriesForFile must filter to one FileId and return oldest-first, since that's the
// order assignTermsForFile's cascading eligibility walk requires.
func TestEntriesForFile_FiltersAndSortsOldestFirst(t *testing.T) {
	s := newRecentVersionStore()
	base := time.Now()

	s.entries = []recentVersion{
		{fileId: "f1", versionId: "v2", path: "a.txt", content: "two", addedAt: base.Add(time.Second)},
		{fileId: "other", versionId: "vX", path: "x.txt", content: "x", addedAt: base},
		{fileId: "f1", versionId: "v1", path: "a.txt", content: "one", addedAt: base},
	}

	out := s.EntriesForFile("f1")
	if len(out) != 2 {
		t.Fatalf("expected 2 entries for f1, got %d: %+v", len(out), out)
	}
	if out[0].versionId != "v1" || out[1].versionId != "v2" {
		t.Fatalf("expected oldest-first order [v1, v2], got %+v", out)
	}
}

func TestEntriesForFile_NoMatchesReturnsEmpty(t *testing.T) {
	s := newRecentVersionStore()
	s.entries = []recentVersion{{fileId: "other", versionId: "vX", addedAt: time.Now()}}

	out := s.EntriesForFile("f1")
	if len(out) != 0 {
		t.Fatalf("expected no entries, got %d", len(out))
	}
}
