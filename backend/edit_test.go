package backend

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExtractRange(t *testing.T) {
	lines := []string{"hello", "world", "foo"}

	t.Run("single line partial", func(t *testing.T) {
		got, ok := extractRange(lines, Pos{Line: 0, Ch: 1}, Pos{Line: 0, Ch: 4})
		if !ok || !slices.Equal(got, []string{"ell"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("multi line", func(t *testing.T) {
		got, ok := extractRange(lines, Pos{Line: 0, Ch: 3}, Pos{Line: 2, Ch: 2})
		if !ok || !slices.Equal(got, []string{"lo", "world", "fo"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("out of range line fails", func(t *testing.T) {
		if _, ok := extractRange(lines, Pos{Line: 0, Ch: 0}, Pos{Line: 5, Ch: 0}); ok {
			t.Fatalf("expected out-of-range failure")
		}
	})

	t.Run("out of range ch fails", func(t *testing.T) {
		if _, ok := extractRange(lines, Pos{Line: 0, Ch: 0}, Pos{Line: 0, Ch: 99}); ok {
			t.Fatalf("expected out-of-range failure")
		}
	})
}

func TestReplaceRange(t *testing.T) {
	t.Run("insert within line", func(t *testing.T) {
		lines := []string{"hello", "world"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 5}, Pos{Line: 0, Ch: 5}, []string{"!"})
		if !ok || !slices.Equal(got, []string{"hello!", "world"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("multi-line replace collapses to fewer lines", func(t *testing.T) {
		lines := []string{"line1", "line2", "line3"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 2}, Pos{Line: 2, Ch: 2}, []string{"X"})
		if !ok || !slices.Equal(got, []string{"liXne3"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("single line replace expands to more lines", func(t *testing.T) {
		lines := []string{"hello world"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 5}, Pos{Line: 0, Ch: 6}, []string{"", ""})
		if !ok || !slices.Equal(got, []string{"hello", "world"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})
}

// A line inserted at the very start should shift every later old line number by one
// in the mapping, while still being reported as unmodified (equal).
func TestBuildLineMapping(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "inserted\nline1\nline2\nline3"
	oldToNew, equalLine := buildLineMapping(old, new)

	for i := 0; i < 3; i++ {
		if !equalLine[i] {
			t.Fatalf("expected old line %d to be unmodified", i)
		}
		if oldToNew[i] != i+1 {
			t.Fatalf("oldToNew[%d] = %d, want %d", i, oldToNew[i], i+1)
		}
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := NewDB(dir)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	hub := NewHub()
	s := &Server{root: dir, db: db, hub: hub, mux: http.NewServeMux()}
	hub.EditHandler = s.HandleEdit
	return s
}

func createTestFile(t *testing.T, s *Server, name, content string) *DBFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.root, name), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fileId, err := s.db.EnsureFileTracked(name)
	if err != nil {
		t.Fatalf("EnsureFileTracked: %v", err)
	}
	f, err := s.db.GetFile(fileId)
	if err != nil || f == nil {
		t.Fatalf("GetFile: %v", err)
	}
	return f
}

// registerTestClient wires a fake connection into the hub's client/subscription maps
// so broadcasts and direct sends can be observed without a real websocket.
func registerTestClient(s *Server, cid, fileId string) chan []byte {
	ch := make(chan []byte, 8)
	s.hub.mu.Lock()
	s.hub.clients[cid] = &wsClient{cid: cid, send: ch}
	s.hub.mu.Unlock()
	s.hub.SetSubscription(cid, fileId)
	return ch
}

func decodeMsg(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	return m
}

// Versions matching: applying the edit directly to the latest content, no merge needed.
func TestHandleEdit_HappyPath(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "notes.txt", "hello\nworld")

	senderCh := registerTestClient(s, "sender", f.FileId)
	otherCh := registerTestClient(s, "other", f.FileId)

	s.HandleEdit("sender", EditMessage{
		FileId:           f.FileId,
		CurrentVersionId: f.VersionId,
		NewVersionId:     "v1",
		From:             Pos{Line: 0, Ch: 5},
		To:               Pos{Line: 0, Ch: 5},
		Text:             []string{"!"},
		Removed:          []string{""},
	})

	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got.Content != "hello!\nworld" || got.VersionId != "v1" {
		t.Fatalf("got content=%q versionId=%q", got.Content, got.VersionId)
	}

	disk, _ := os.ReadFile(filepath.Join(s.root, "notes.txt"))
	if string(disk) != "hello!\nworld" {
		t.Fatalf("disk content = %q", disk)
	}

	select {
	case raw := <-senderCh:
		t.Fatalf("sender should receive nothing on success, got %s", raw)
	default:
	}

	select {
	case raw := <-otherCh:
		m := decodeMsg(t, raw)
		if m["type"] != "content" || m["content"] != "hello!\nworld" || m["versionId"] != "v1" {
			t.Fatalf("unexpected broadcast: %v", m)
		}
	default:
		t.Fatalf("expected a broadcast to the other subscribed client")
	}
}

// No snapshot exists for the cited CurrentVersionId at all: unresolvable, must bounce.
func TestHandleEdit_ConflictNoSnapshot(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "notes.txt", "hello\nworld")
	senderCh := registerTestClient(s, "sender", f.FileId)

	s.HandleEdit("sender", EditMessage{
		FileId:           f.FileId,
		CurrentVersionId: "never-seen-version",
		NewVersionId:     "v1",
		From:             Pos{Line: 0, Ch: 0},
		To:               Pos{Line: 0, Ch: 0},
		Text:             []string{"X"},
		Removed:          []string{""},
	})

	got, _ := s.db.GetFile(f.FileId)
	if got.Content != "hello\nworld" || got.VersionId != f.VersionId {
		t.Fatalf("DB should be unchanged, got %+v", got)
	}

	m := decodeMsg(t, <-senderCh)
	if m["type"] != "editConflict" || m["content"] != "hello\nworld" || m["versionId"] != f.VersionId {
		t.Fatalf("unexpected message: %v", m)
	}
}

// A snapshot exists, but the client's "removed" doesn't match what's actually there:
// treated the same as a true conflict, per spec.
func TestHandleEdit_ConflictRemovedMismatch(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "notes.txt", "hello\nworld")

	s.hub.Versions.Add(f.FileId, "snap1", f.Path, "hello\nworld")
	ok, err := s.db.UpdateContentAndVersionIf(f.FileId, "hello\nworld!!", "latest1", f.VersionId)
	if err != nil || !ok {
		t.Fatalf("setup update failed: ok=%v err=%v", ok, err)
	}

	senderCh := registerTestClient(s, "sender", f.FileId)

	s.HandleEdit("sender", EditMessage{
		FileId:           f.FileId,
		CurrentVersionId: "snap1",
		NewVersionId:     "v2",
		From:             Pos{Line: 1, Ch: 0},
		To:               Pos{Line: 1, Ch: 5},
		Text:             []string{"WORLD"},
		Removed:          []string{"WRONG"}, // snapshot actually has "world" here
	})

	got, _ := s.db.GetFile(f.FileId)
	if got.Content != "hello\nworld!!" || got.VersionId != "latest1" {
		t.Fatalf("DB should be unchanged, got %+v", got)
	}

	m := decodeMsg(t, <-senderCh)
	if m["type"] != "editConflict" || m["versionId"] != "latest1" {
		t.Fatalf("unexpected message: %v", m)
	}
}

// The snapshot is stale (another edit inserted an earlier line, shifting everything
// after it down by one) but the client's edit targets a line entirely outside the
// changed hunk, so it should relocate and merge cleanly.
func TestHandleEdit_RelocationSuccess(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "notes.txt", "line1\nline2\nline3")

	snapshot := "line1\nline2\nline3"
	s.hub.Versions.Add(f.FileId, "snap1", f.Path, snapshot)
	latestContent := "inserted\nline1\nline2\nline3"
	ok, err := s.db.UpdateContentAndVersionIf(f.FileId, latestContent, "latest1", f.VersionId)
	if err != nil || !ok {
		t.Fatalf("setup update failed: ok=%v err=%v", ok, err)
	}

	senderCh := registerTestClient(s, "sender", f.FileId)
	otherCh := registerTestClient(s, "other", f.FileId)

	s.HandleEdit("sender", EditMessage{
		FileId:           f.FileId,
		CurrentVersionId: "snap1",
		NewVersionId:     "v2",
		From:             Pos{Line: 2, Ch: 0},
		To:               Pos{Line: 2, Ch: 5},
		Text:             []string{"LINE3"},
		Removed:          []string{"line3"},
	})

	wantMerged := "inserted\nline1\nline2\nLINE3"
	got, err := s.db.GetFile(f.FileId)
	if err != nil || got == nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got.Content != wantMerged {
		t.Fatalf("content = %q, want %q", got.Content, wantMerged)
	}
	if got.VersionId == "v2" || got.VersionId == "latest1" || got.VersionId == "" {
		t.Fatalf("expected a fresh server-minted versionId, got %q", got.VersionId)
	}
	freshVersionId := got.VersionId

	naive, found := s.hub.Versions.Lookup(f.FileId, "v2")
	if !found || naive != "line1\nline2\nLINE3" {
		t.Fatalf("naive snapshot under client's NewVersionId = (%q, %v)", naive, found)
	}

	for _, ch := range []chan []byte{senderCh, otherCh} {
		m := decodeMsg(t, <-ch)
		if m["type"] != "content" || m["content"] != wantMerged || m["versionId"] != freshVersionId {
			t.Fatalf("unexpected broadcast: %v", m)
		}
	}
}

// The snapshot is stale because another edit modified the very line the client is
// also trying to edit: the hunks overlap, so relocation must fail as a true conflict.
func TestHandleEdit_RelocationOverlapConflict(t *testing.T) {
	s := newTestServer(t)
	f := createTestFile(t, s, "notes.txt", "line1\nline2\nline3")

	s.hub.Versions.Add(f.FileId, "snap1", f.Path, "line1\nline2\nline3")
	latestContent := "line1\nCHANGED\nline3"
	ok, err := s.db.UpdateContentAndVersionIf(f.FileId, latestContent, "latest1", f.VersionId)
	if err != nil || !ok {
		t.Fatalf("setup update failed: ok=%v err=%v", ok, err)
	}

	senderCh := registerTestClient(s, "sender", f.FileId)

	s.HandleEdit("sender", EditMessage{
		FileId:           f.FileId,
		CurrentVersionId: "snap1",
		NewVersionId:     "v2",
		From:             Pos{Line: 1, Ch: 0},
		To:               Pos{Line: 1, Ch: 5},
		Text:             []string{"edited"},
		Removed:          []string{"line2"},
	})

	got, _ := s.db.GetFile(f.FileId)
	if got.Content != latestContent || got.VersionId != "latest1" {
		t.Fatalf("DB should be unchanged, got %+v", got)
	}

	m := decodeMsg(t, <-senderCh)
	if m["type"] != "editConflict" || m["content"] != latestContent || m["versionId"] != "latest1" {
		t.Fatalf("unexpected message: %v", m)
	}
}
