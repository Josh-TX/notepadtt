package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func editAt(s *Server, id, ver, newVer string, line, ch int, text string) {
	s.HandleEdit("c1", EditMessage{
		FileId: id, CurrentVersionId: ver, NewVersionId: newVer,
		From: Pos{line, ch}, To: Pos{line, ch}, Text: []string{text}, Removed: []string{""},
	})
}

// A missed fsnotify event must not let a flush clobber newer disk content.
func TestFlushMergesMissedExternalChange(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "one\ntwo\nthree")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "")

	// external edit, no syncOpenPath call: simulates a dropped event
	writeFile(t, s, "a.txt", "one\ntwo\nthree\nfour")
	editAt(s, a.FileId, f.VersionId, "v2", 0, 3, "!")
	s.FlushAll()

	b, _ := os.ReadFile(s.abs("a.txt"))
	if string(b) != "one!\ntwo\nthree\nfour" {
		t.Fatalf("disk = %q", b)
	}
	if got := getFile(t, s, a.FileId, ""); got.Content != string(b) {
		t.Fatalf("memory %q != disk %q", got.Content, b)
	}
}

// An unmergeable overlap keeps the disk version in a visible conflict copy.
func TestFlushConflictKeepsDiskCopy(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "hello")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "")

	editAt(s, a.FileId, f.VersionId, "v2", 0, 5, " world")
	writeFile(t, s, "a.txt", "completely different")
	s.FlushAll()

	matches, _ := filepath.Glob(filepath.Join(s.rootAbs, "a (disk copy *).txt"))
	if len(matches) != 1 {
		t.Fatalf("expected one conflict copy, got %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "completely different" {
		t.Fatalf("conflict copy = %q", b)
	}
}

// reconcile pulls in external changes with no watcher event at all.
func TestReconcileDetectsMissedChange(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "one")
	a := openTab(t, s, "a.txt")
	ch := registerClient(s, "c2")
	getFile(t, s, a.FileId, "?cid=c2")

	writeFile(t, s, "a.txt", "two")
	s.mu.Lock()
	s.reconcileLocked(s.byId[a.FileId])
	s.mu.Unlock()
	msgs := drain(ch)
	if len(msgs) != 1 || msgs[0]["content"] != "two" {
		t.Fatalf("got %v", msgs)
	}
}

// Same size, same mtime: only a content compare can notice.
func TestReconcileDetectsSameSizeSameMtime(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "aaa")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "")
	_ = f
	info, _ := os.Stat(s.abs("a.txt"))
	writeFile(t, s, "a.txt", "bbb")
	os.Chtimes(s.abs("a.txt"), time.Now(), info.ModTime())

	s.mu.Lock()
	s.reconcileLocked(s.byId[a.FileId])
	got := s.byId[a.FileId].content
	s.mu.Unlock()
	if got != "bbb" {
		t.Fatalf("content = %q", got)
	}
}

func TestAtomicWriteLeavesNoTmpAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	os.WriteFile(p, []byte("old"), 0640)
	os.Chmod(p, 0640)
	if _, err := writeFileAtomic(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0640 {
		t.Fatalf("mode %v", info.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestAtomicWriteThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	os.WriteFile(real, []byte("old"), 0644)
	os.Symlink(real, link)
	if _, err := writeFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Fatalf("real = %q", b)
	}
}

func TestTmpSuffixHiddenAndCleanedAtStartup(t *testing.T) {
	if !isExcludedName("foo.txt"+tmpSuffix) || validRel("a/foo"+tmpSuffix) {
		t.Fatal("tmp names should be excluded")
	}
	s := newTestServer(t)
	stale := filepath.Join(s.rootAbs, ".a.txt.abc"+tmpSuffix)
	writeFile(t, s, "a.txt", "x")
	os.WriteFile(stale, []byte("junk"), 0644)
	removeStaleTmpFiles(s.rootAbs)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale tmp not removed")
	}
	tree := do(t, s, "GET", "/api/tree", nil).Body.String()
	if strings.Contains(tree, tmpSuffix) {
		t.Fatalf("tree shows tmp: %s", tree)
	}
}
