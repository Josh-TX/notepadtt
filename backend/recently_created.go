package backend

import (
	"sync"
	"time"
)

type recentlyCreatedEntry struct {
	path    string
	addedAt time.Time
}

// RecentlyCreatedStore tracks file paths an API handler just wrote to disk and
// is about to (or just did) insert into the DB. The fsnotify watcher checks this
// before tracking a Create event itself, so the two don't both insert a DB row
// for the same path. Entries expire after 1 second.
type RecentlyCreatedStore struct {
	mu      sync.Mutex
	entries []recentlyCreatedEntry
}

func newRecentlyCreatedStore() *RecentlyCreatedStore {
	s := &RecentlyCreatedStore{}
	go s.cleanupLoop()
	return s
}

func (s *RecentlyCreatedStore) Mark(path string) {
	s.mu.Lock()
	s.entries = append(s.entries, recentlyCreatedEntry{path, time.Now()})
	s.mu.Unlock()
}

// Contains reports whether path was marked within the last second.
func (s *RecentlyCreatedStore) Contains(path string) bool {
	cutoff := time.Now().Add(-1 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.path == path && e.addedAt.After(cutoff) {
			return true
		}
	}
	return false
}

func (s *RecentlyCreatedStore) cleanupLoop() {
	for range time.Tick(time.Minute) {
		s.cleanup()
	}
}

func (s *RecentlyCreatedStore) cleanup() {
	cutoff := time.Now().Add(-1 * time.Second)
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
