package server

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	flushDelay   = 200 * time.Millisecond // idle time after the last edit before writing
	flushMaxWait = 1 * time.Second        // longest an edit may sit unwritten

	reconcileInterval = 5 * time.Second // backstop for missed fsnotify events
	racyWindow        = 2 * time.Second // mtimes this fresh can't prove "unchanged"
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
	f.dirty = false
	f.flushing.Add(1)
	f.writeMu.Lock()
	content := s.guardedContentLocked(f) // may fold in external disk changes
	abs := s.abs(f.path)
	s.mu.Unlock()

	sig, err := writeFileAtomic(abs, []byte(content))
	if err == nil {
		f.base, f.sig = content, sig
	}
	f.writeMu.Unlock()
	f.flushing.Add(-1)
	if err != nil {
		log.Printf("[save] write %s: %v", abs, err)
		s.mu.Lock()
		if s.byId[id] == f && !f.dirty {
			s.markDirtyLocked(f) // retry; content in memory is still the truth
		}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.reconcileLocked(f) // catch a change that landed while we were writing
	s.mu.Unlock()
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
	content := s.guardedContentLocked(f)
	sig, err := writeFileAtomic(s.abs(f.path), []byte(content))
	if err != nil {
		log.Printf("[save] write %s: %v", f.path, err)
		return
	}
	f.base, f.sig = content, sig
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

// ---- disk-state guard ----

// diskSig is a cheap fingerprint of a file's on-disk state.
type diskSig struct {
	mtime time.Time
	size  int64
	taken time.Time // when recorded; see sigMatches
}

func statSig(abs string) (diskSig, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return diskSig{}, err
	}
	return diskSig{mtime: info.ModTime(), size: info.Size(), taken: time.Now()}, nil
}

// diskUnchanged reports whether abs still looks like what we last saw. A matching
// stat is trusted only if the mtime was old when recorded: a write within the
// filesystem's timestamp granularity of our own could keep the same mtime+size.
// Otherwise fall back to comparing contents.
func (f *openFile) diskUnchanged(abs string) bool {
	cur, err := statSig(abs)
	if err != nil {
		return false
	}
	if cur.mtime.Equal(f.sig.mtime) && cur.size == f.sig.size &&
		f.sig.taken.Sub(f.sig.mtime) > racyWindow {
		return true
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return false
	}
	if string(b) == f.base {
		f.sig = cur
		return true
	}
	return false
}

// guardedContentLocked returns what should be written for f. If disk still matches
// what we last saw it is just f.content. If it was changed behind our back (a
// missed fsnotify event, or one skipped while dirty), our edits are three-way
// merged onto the disk version; if that can't be done cleanly the disk version is
// preserved in a conflict copy and our content wins. Needs s.mu and f.writeMu.
func (s *Server) guardedContentLocked(f *openFile) string {
	abs := s.abs(f.path)
	if _, err := os.Stat(abs); err != nil || f.diskUnchanged(abs) {
		return f.content // missing file is simply recreated
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return f.content
	}
	disk := string(b)
	log.Printf("[save] %s changed on disk behind our back; merging", f.path)

	dmp := diffmatchpatch.New()
	dmp.MatchThreshold = 0 // exact context only: no fuzzy placement of our edits
	patches := dmp.PatchMake(f.base, f.content)
	merged, applied := dmp.PatchApply(patches, disk)
	ok := true
	for _, a := range applied {
		ok = ok && a
	}
	if !ok {
		conflict := conflictCopyPath(abs)
		if err := os.WriteFile(conflict, b, 0644); err != nil {
			log.Printf("[save] could not preserve disk version of %s: %v", f.path, err)
		} else {
			log.Printf("[save] merge of %s failed; disk version kept at %s", f.path, conflict)
		}
		return f.content
	}
	if merged != f.content {
		f.content = merged
		f.versionId = uniqueId(5)
		s.hub.Versions.Add(f.id, f.versionId, merged)
		s.hub.BroadcastContent(f.id, merged, f.versionId, "")
	}
	return merged
}

func conflictCopyPath(abs string) string {
	ext := filepath.Ext(abs)
	return strings.TrimSuffix(abs, ext) + " (disk copy " + time.Now().Format("2006-01-02 150405") + ")" + ext
}

// reconcileLocked is the backstop for lost fsnotify events: if f's file changed on
// disk since we last saw it, pull it in. Cheap (one stat) when nothing changed.
func (s *Server) reconcileLocked(f *openFile) {
	if !f.loaded || f.dirty || f.flushing.Load() > 0 {
		return
	}
	if !f.diskUnchanged(s.abs(f.path)) {
		s.syncFromDiskLocked(f)
	}
}

func (s *Server) reconcileLoop() {
	for range time.Tick(reconcileInterval) {
		s.mu.Lock()
		for _, f := range s.byId {
			s.reconcileLocked(f)
		}
		s.mu.Unlock()
	}
}

// ---- atomic writes ----

// writeFileAtomic replaces abs via a temp file (tmpSuffix, ignored everywhere) in
// the same directory plus rename, so readers never see a partial file. Symlinked
// targets are written through; the existing file's permission bits are kept. It
// returns the new file's signature.
func writeFileAtomic(abs string, data []byte) (diskSig, error) {
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	mode := fs.FileMode(0644)
	if info, err := os.Stat(abs); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(abs), "."+filepath.Base(abs)+"."+uniqueId(8)+tmpSuffix)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return diskSig{}, err
	}
	fail := func(err error) (diskSig, error) {
		f.Close()
		os.Remove(tmp)
		return diskSig{}, err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	info, err := f.Stat() // rename preserves mtime/size, so this is the final signature
	if err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return diskSig{}, err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return diskSig{}, err
	}
	return diskSig{mtime: info.ModTime(), size: info.Size(), taken: time.Now()}, nil
}

// removeStaleTmpFiles deletes temp files left behind by a crash mid-write.
func removeStaleTmpFiles(root string) {
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), tmpSuffix) {
			os.Remove(p)
		}
		return nil
	})
}
