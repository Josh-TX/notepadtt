package backend

import (
	"sync"
	"time"
)

type recentVersion struct {
	fileId    string
	versionId string
	content   string
	addedAt   time.Time
}

// RecentVersionStore holds recent content snapshots keyed by (fileId, versionId).
// Used to retrieve the "old" content a client was working from during conflict
// resolution. Entries expire after 5 seconds; a ticker purges them.
type RecentVersionStore struct {
	mu      sync.Mutex
	entries []recentVersion
}

func newRecentVersionStore() *RecentVersionStore {
	s := &RecentVersionStore{}
	go func() {
		for range time.Tick(time.Second) {
			s.cleanup()
		}
	}()
	return s
}

func (s *RecentVersionStore) Add(fileId, versionId, content string) {
	s.mu.Lock()
	s.entries = append(s.entries, recentVersion{fileId, versionId, content, time.Now()})
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

// Forget drops all entries for a file (used when its tab closes).
func (s *RecentVersionStore) Forget(fileId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.fileId != fileId {
			s.entries[n] = e
			n++
		}
	}
	s.entries = s.entries[:n]
}

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
