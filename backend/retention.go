package backend

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Retention tier durations. TTL controls when a tier's FileVersions rows expire;
// MinDelay gates how soon after the last persisted same-or-higher tier snapshot a new
// one may be promoted into it. Values are loaded from Settings (see
// applyRetentionSettings) at startup and on every settings save — these vars are no
// longer hardcoded.
var (
	ShortTermTTL      time.Duration
	ShortTermMinDelay time.Duration
	MedTermTTL        time.Duration
	MedTermMinDelay   time.Duration
	LongTermTTL       time.Duration
	LongTermMinDelay  time.Duration

	TrashTTL time.Duration
)

// minDelayByTerm[N] is the MinDelay for cumulative term N (1..3); index 0 is unused.
// Re-derived by applyRetentionSettings whenever the MinDelay vars above change.
var minDelayByTerm [4]time.Duration

// applyRetentionSettings parses TrashTTL and the 6 File History TTL/MinDelay strings
// from Settings into the package vars above. Called from setSettingsCache, which holds
// settingsMu for the duration — that's the only synchronization here: read access from
// the once-a-minute FileVersioning tick is intentionally left unguarded, since the only
// writer is a rare admin save and a stale read during that narrow window is a harmless,
// momentary inconsistency, not worth a read-side lock on every tick.
func applyRetentionSettings(s Settings) error {
	var err error
	if ShortTermTTL, err = parseDuration(s.ShortTermTTL); err != nil {
		return err
	}
	if ShortTermMinDelay, err = parseDuration(s.ShortTermMinDelay); err != nil {
		return err
	}
	if MedTermTTL, err = parseDuration(s.MedTermTTL); err != nil {
		return err
	}
	if MedTermMinDelay, err = parseDuration(s.MedTermMinDelay); err != nil {
		return err
	}
	if LongTermTTL, err = parseDuration(s.LongTermTTL); err != nil {
		return err
	}
	if LongTermMinDelay, err = parseDuration(s.LongTermMinDelay); err != nil {
		return err
	}
	if TrashTTL, err = parseDuration(s.TrashTTL); err != nil {
		return err
	}
	minDelayByTerm = [4]time.Duration{0, ShortTermMinDelay, MedTermMinDelay, LongTermMinDelay}
	return nil
}

// parseDuration parses a duration with a single case-insensitive unit (s/m/h/d),
// optionally separated from the number by a space, e.g. "8m", "8 M", "7d".
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	unit := strings.ToLower(s[len(s)-1:])
	numPart := strings.TrimSpace(s[:len(s)-1])
	n, err := strconv.Atoi(numPart)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	switch unit {
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit %q in %q", unit, s)
	}
}

// StartFileVersioning launches the once-a-minute background process that expires
// aged-out FileVersions rows and persists new tiered snapshots sourced from
// versions' current entries. This replaces RecentVersionStore's old standalone
// cleanup ticker — the 5s purge of stale entries now runs as this process's final
// step (runFileVersioningOnce), after it's had a chance to persist from them.
func StartFileVersioning(db *DB, versions *RecentVersionStore) {
	go func() {
		for range time.Tick(time.Minute) {
			runFileVersioningOnce(db, versions)
		}
	}()
}

func runFileVersioningOnce(db *DB, versions *RecentVersionStore) {
	if err := db.DeleteExpiredFileVersions(time.Now()); err != nil {
		log.Printf("[retention] delete expired file versions: %v", err)
	}
	if err := db.DeleteExpiredFileTrash(time.Now()); err != nil {
		log.Printf("[retention] delete expired file trash: %v", err)
	}

	entries := versions.Entries()
	byFileId := map[string][]recentVersion{}
	var fileIds []string
	for _, e := range entries {
		if _, ok := byFileId[e.fileId]; !ok {
			fileIds = append(fileIds, e.fileId)
		}
		byFileId[e.fileId] = append(byFileId[e.fileId], e)
	}

	lastDates, err := db.GetLastVersionDates(fileIds)
	if err != nil {
		log.Printf("[retention] get last version dates: %v", err)
		versions.cleanup()
		return
	}

	for _, fileId := range fileIds {
		fileEntries := byFileId[fileId]
		sort.Slice(fileEntries, func(i, j int) bool {
			return fileEntries[i].addedAt.Before(fileEntries[j].addedAt)
		})
		last := lastDates[fileId]
		for _, fv := range assignTermsForFile(fileEntries, &last) {
			if err := db.InsertFileVersion(fv); err != nil {
				log.Printf("[retention] insert file version: %v", err)
			}
		}
	}

	versions.cleanup()
}

// assignTermsForFile walks one FileId's RecentFileVersions entries (oldest first)
// and returns the FileVersions rows to persist, mutating last in place so later
// entries in the same call see watermarks updated by earlier ones.
//
// last[N-1] is the most recent Date among already-persisted rows satisfying Term>=N
// (seeded from the DB, then updated as entries here get assigned a Term). A zero
// value means no qualifying row exists yet, which always clears that tier's gate.
func assignTermsForFile(entries []recentVersion, last *lastVersionDates) []FileVersion {
	var out []FileVersion
	for _, e := range entries {
		addedAtMillis := e.addedAt.UnixMilli()

		if last[0] != 0 && gap(addedAtMillis, last[0]) < ShortTermMinDelay {
			continue
		}

		term := 1
		for n := 2; n <= 3; n++ {
			if last[n-1] != 0 && gap(addedAtMillis, last[n-1]) < minDelayByTerm[n] {
				break
			}
			term = n
		}

		out = append(out, FileVersion{
			FileId:    e.fileId,
			Path:      e.path,
			Content:   e.content,
			VersionId: e.versionId,
			Date:      addedAtMillis,
			Term:      term,
		})

		for n := 1; n <= term; n++ {
			last[n-1] = addedAtMillis
		}
	}
	return out
}

func gap(addedAtMillis, lastMillis int64) time.Duration {
	return time.Duration(addedAtMillis-lastMillis) * time.Millisecond
}
