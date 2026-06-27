package backend

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"10m", 10 * time.Minute},
		{"40s", 40 * time.Second},
		{"8m", 8 * time.Minute},
		{"8 m", 8 * time.Minute},
		{"8 M", 8 * time.Minute}, // case-insensitive unit
		{"7d", 7 * 24 * time.Hour},
		{"18h", 18 * time.Hour},
		{"60d", 60 * 24 * time.Hour},
		{"5d", 5 * 24 * time.Hour},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if err != nil {
			t.Fatalf("parseDuration(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	invalid := []string{"", "10", "m", "10x", "abc"}
	for _, in := range invalid {
		if _, err := parseDuration(in); err == nil {
			t.Fatalf("parseDuration(%q) expected error, got nil", in)
		}
	}
}

// runFileVersioningOnce also sweeps FileTrash on every tick, so an aged-out trash row
// gets purged in the same pass that handles FileVersions expiry.
func TestRunFileVersioningOnce_PurgesExpiredFileTrash(t *testing.T) {
	db := newTestDB(t)
	versions := newRecentVersionStore()
	old := time.Now().Add(-TrashTTL - time.Minute).UnixMilli()
	fileId := trashTestFile(t, db, "a.txt", "hello", old)

	runFileVersioningOnce(db, versions)

	if _, found, _ := db.GetFileTrash(fileId); found {
		t.Fatalf("expected expired trash row purged by runFileVersioningOnce")
	}
}

func recentAt(fileId, versionId, path, content string, at time.Time) recentVersion {
	return recentVersion{fileId: fileId, versionId: versionId, path: path, content: content, addedAt: at}
}

// The very first snapshot ever taken for a file has no prior watermark at any tier,
// so the gap against every tier is "infinite" and it should be promoted straight to
// the top tier (4) rather than starting conservatively at 1.
func TestAssignTermsForFile_FirstEntryGetsTopTerm(t *testing.T) {
	base := time.Now()
	entries := []recentVersion{recentAt("f1", "v1", "a.txt", "hello", base)}
	var last lastVersionDates

	out := assignTermsForFile(entries, &last)
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	if out[0].Term != 4 {
		t.Fatalf("expected Term=4 for first-ever entry, got %d", out[0].Term)
	}
	if last[3] != base.UnixMilli() {
		t.Fatalf("expected last[3] updated to base, got %d", last[3])
	}
}

// An entry arriving before ShortTermMinDelay has elapsed since the last Term>=1 row
// must be skipped entirely (not persisted at any tier).
func TestAssignTermsForFile_TooSoonSkipped(t *testing.T) {
	base := time.Now()
	var last lastVersionDates
	last[0] = base.UnixMilli() // pretend a Term>=1 row already exists right now

	entries := []recentVersion{recentAt("f1", "v2", "a.txt", "world", base.Add(10*time.Second))}
	out := assignTermsForFile(entries, &last)
	if len(out) != 0 {
		t.Fatalf("expected entry within ShortTermMinDelay to be skipped, got %d rows", len(out))
	}
}

// Gap clears ShortTermMinDelay but not MedTermMinDelay: row persists at Term=1 only.
func TestAssignTermsForFile_ShortTermOnly(t *testing.T) {
	entryTime := time.Now()
	var last lastVersionDates
	// Tier 1 watermark old enough to clear ShortTermMinDelay; tiers 2-4 watermarks
	// recent (as a real prior promotion to Term>=2 would leave them), too recent to
	// clear MedTermMinDelay.
	last[0] = entryTime.Add(-(ShortTermMinDelay + time.Second)).UnixMilli()
	last[1] = entryTime.Add(-time.Second).UnixMilli()
	last[2] = last[1]
	last[3] = last[1]

	entries := []recentVersion{recentAt("f1", "v2", "a.txt", "world", entryTime)}
	out := assignTermsForFile(entries, &last)
	if len(out) != 1 || out[0].Term != 1 {
		t.Fatalf("expected single Term=1 row, got %+v", out)
	}
}

// Gap clears MedTermMinDelay but not LongTermMinDelay: row promotes to Term=2.
func TestAssignTermsForFile_PromotesToMedium(t *testing.T) {
	entryTime := time.Now()
	var last lastVersionDates
	last[0] = entryTime.Add(-(MedTermMinDelay + time.Second)).UnixMilli()
	last[1] = last[0]
	last[2] = entryTime.Add(-time.Second).UnixMilli()
	last[3] = last[2]

	entries := []recentVersion{recentAt("f1", "v2", "a.txt", "world", entryTime)}
	out := assignTermsForFile(entries, &last)
	if len(out) != 1 || out[0].Term != 2 {
		t.Fatalf("expected single Term=2 row, got %+v", out)
	}
}

// Gap clears LongTermMinDelay but not VeryLongTermMinDelay: row promotes to Term=3.
func TestAssignTermsForFile_PromotesToLong(t *testing.T) {
	entryTime := time.Now()
	var last lastVersionDates
	last[0] = entryTime.Add(-(LongTermMinDelay + time.Minute)).UnixMilli()
	last[1] = last[0]
	last[2] = last[0]
	last[3] = entryTime.Add(-time.Minute).UnixMilli()

	entries := []recentVersion{recentAt("f1", "v2", "a.txt", "world", entryTime)}
	out := assignTermsForFile(entries, &last)
	if len(out) != 1 || out[0].Term != 3 {
		t.Fatalf("expected single Term=3 row, got %+v", out)
	}
}

// Gap clears even VeryLongTermMinDelay: row promotes all the way to Term=4.
func TestAssignTermsForFile_PromotesToVeryLong(t *testing.T) {
	base := time.Now()
	var last lastVersionDates
	last[0], last[1], last[2], last[3] = base.UnixMilli(), base.UnixMilli(), base.UnixMilli(), base.UnixMilli()

	entries := []recentVersion{recentAt("f1", "v2", "a.txt", "world", base.Add(VeryLongTermMinDelay+time.Hour))}
	out := assignTermsForFile(entries, &last)
	if len(out) != 1 || out[0].Term != 4 {
		t.Fatalf("expected single Term=4 row, got %+v", out)
	}
}

// Two entries within the same run, both far enough apart to each independently clear
// ShortTermMinDelay, both get persisted (future-proofing case called out by the spec:
// RecentFileVersions' retention window or ShortTermMinDelay may change later).
func TestAssignTermsForFile_MultipleRowsInSingleRun(t *testing.T) {
	base := time.Now()
	var last lastVersionDates

	entries := []recentVersion{
		recentAt("f1", "v1", "a.txt", "one", base),
		recentAt("f1", "v2", "a.txt", "two", base.Add(ShortTermMinDelay+time.Second)),
	}
	out := assignTermsForFile(entries, &last)
	if len(out) != 2 {
		t.Fatalf("expected 2 persisted rows, got %d: %+v", len(out), out)
	}
}
