package backend

import (
	"sort"
	"sync"
	"time"
)

type recentVersion struct {
	fileId    string
	versionId string
	path      string
	content   string
	addedAt   time.Time
}

// RecentVersionStore holds recent content snapshots keyed by (fileId, versionId).
// Used to retrieve the "old" content a client was working from during 3-way merge,
// and read by the FileVersioning process (retention.go) as the source of every
// FileVersions row it persists. Entries expire after 5 seconds; purged as the final
// step of that process's once-a-minute run rather than by a standalone ticker.
type RecentVersionStore struct {
	mu      sync.Mutex
	entries []recentVersion
}

func newRecentVersionStore() *RecentVersionStore {
	return &RecentVersionStore{}
}

// Add records a content snapshot. path is the file's path at the time of this
// snapshot, needed because FileVersions rows persisted from this entry require one.
func (s *RecentVersionStore) Add(fileId, versionId, path, content string) {
	s.mu.Lock()
	s.entries = append(s.entries, recentVersion{fileId, versionId, path, content, time.Now()})
	s.mu.Unlock()
}

// Lookup returns the content for (fileId, versionId) if it exists and is not expired.
func (s *RecentVersionStore) Lookup(fileId, versionId string) (string, bool) {
	cutoff := time.Now().Add(-5 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.fileId == fileId && e.versionId == versionId && e.addedAt.After(cutoff) {
			return e.content, true
		}
	}
	return "", false
}

// Entries returns a snapshot copy of all current entries, regardless of age, for the
// FileVersioning process to evaluate as FileVersions candidates.
func (s *RecentVersionStore) Entries() []recentVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recentVersion, len(s.entries))
	copy(out, s.entries)
	return out
}

// EntriesForFile returns a snapshot of one FileId's entries, oldest first, regardless
// of age — the shape assignTermsForFile expects, for both the FileVersioning process
// and the on-demand GET /api/files/{id}/versions handler to evaluate as candidates.
func (s *RecentVersionStore) EntriesForFile(fileId string) []recentVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recentVersion
	for _, e := range s.entries {
		if e.fileId == fileId {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].addedAt.Before(out[j].addedAt) })
	return out
}

// cleanup purges entries older than 5 seconds. Called as the final step of the
// FileVersioning process (retention.go) rather than its own ticker.
func (s *RecentVersionStore) cleanup() {
	cutoff := time.Now().Add(-5 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.addedAt.After(cutoff) {
			s.entries[n] = e
			n++
		}
	}
	s.entries = s.entries[:n]
}
