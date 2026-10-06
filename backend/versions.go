package backend

import "sync"

const (
	versionsPerFile = 16
	versionsMaxSize = 8 << 20 // bytes per file; the newest 2 are always kept
)

type recentVersion struct {
	versionId string
	content   string
}

// RecentVersionStore holds the last few content snapshots per file, keyed by
// (fileId, versionId). Used to retrieve the "old" content a client was working
// from during conflict resolution. Bounded by count and size per file, not time.
type RecentVersionStore struct {
	mu    sync.Mutex
	files map[string][]recentVersion // oldest first
}

func newRecentVersionStore() *RecentVersionStore {
	return &RecentVersionStore{files: map[string][]recentVersion{}}
}

func (s *RecentVersionStore) Add(fileId, versionId, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.files[fileId]
	for i, e := range list {
		if e.versionId == versionId {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	list = append(list, recentVersion{versionId, content})
	size := 0
	for _, e := range list {
		size += len(e.content)
	}
	drop := 0
	for drop < len(list)-2 && (len(list)-drop > versionsPerFile || size > versionsMaxSize) {
		size -= len(list[drop].content)
		drop++
	}
	if drop > 0 {
		list = append(list[:0], list[drop:]...)
	}
	s.files[fileId] = list
}

// Lookup returns the content for (fileId, versionId) if it is still retained.
func (s *RecentVersionStore) Lookup(fileId, versionId string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.files[fileId] {
		if e.versionId == versionId {
			return e.content, true
		}
	}
	return "", false
}

// Forget drops all entries for a file (used when its tab closes).
func (s *RecentVersionStore) Forget(fileId string) {
	s.mu.Lock()
	delete(s.files, fileId)
	s.mu.Unlock()
}
