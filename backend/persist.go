package backend

import (
	"log"
	"os"
	"time"
)

const (
	flushDelay   = 200 * time.Millisecond // idle time after the last edit before writing
	flushMaxWait = 1 * time.Second        // longest an edit may sit unwritten
)

// markDirtyLocked records that f's memory content is ahead of disk and (re)arms
// its flush timer. Edits are written behind, not on every keystroke.
func (s *Server) markDirtyLocked(f *openFile) {
	now := time.Now()
	if !f.dirty {
		f.dirty = true
		f.dirtySince = now
	}
	d := min(flushDelay, max(flushMaxWait-now.Sub(f.dirtySince), 0))
	if f.flushTimer != nil {
		f.flushTimer.Stop()
	}
	id := f.id
	f.flushTimer = time.AfterFunc(d, func() { s.flushAsync(id) })
}

// flushAsync writes a dirty file without holding s.mu during the disk write. Taking
// writeMu before releasing s.mu keeps writes ordered and lets flushLocked (rename,
// delete, ...) wait for an in-flight write.
func (s *Server) flushAsync(id string) {
	s.mu.Lock()
	f := s.byId[id]
	if f == nil || !f.dirty {
		s.mu.Unlock()
		return
	}
	content, abs := f.content, s.abs(f.path)
	f.dirty = false
	f.flushing.Add(1)
	f.writeMu.Lock()
	s.mu.Unlock()

	err := os.WriteFile(abs, []byte(content), 0644)
	f.writeMu.Unlock()
	f.flushing.Add(-1)
	if err != nil {
		log.Printf("[save] write %s: %v", abs, err)
		s.mu.Lock()
		if s.byId[id] == f && !f.dirty {
			s.markDirtyLocked(f) // retry; content in memory is still the truth
		}
		s.mu.Unlock()
	}
}

// flushLocked synchronously writes f if dirty, after waiting out any in-flight
// write. Call before anything that reads or moves the file on disk. On failure f
// stays dirty.
func (s *Server) flushLocked(f *openFile) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if !f.dirty {
		return
	}
	if err := os.WriteFile(s.abs(f.path), []byte(f.content), 0644); err != nil {
		log.Printf("[save] write %s: %v", f.path, err)
		return
	}
	f.dirty = false
	if f.flushTimer != nil {
		f.flushTimer.Stop()
	}
}

// FlushAll writes every dirty file; call on shutdown.
func (s *Server) FlushAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.byId {
		s.flushLocked(f)
	}
}

// flushPath flushes the open file at rel, if any.
func (s *Server) flushPath(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.byPath[rel]; f != nil {
		s.flushLocked(f)
	}
}
