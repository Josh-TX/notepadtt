package backend

import (
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(t.TempDir(), embed.FS{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func do(t *testing.T, s *Server, method, url string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, url, rd)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func writeFile(t *testing.T, s *Server, rel, content string) {
	t.Helper()
	abs := s.abs(rel)
	os.MkdirAll(filepath.Dir(abs), 0755)
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// registerClient adds a fake WS connection and returns its outbound channel.
func registerClient(s *Server, cid string) chan []byte {
	ch := make(chan []byte, 64)
	s.hub.mu.Lock()
	s.hub.clients[cid] = &wsClient{cid: cid, send: ch}
	s.hub.mu.Unlock()
	return ch
}

func openTab(t *testing.T, s *Server, rel string) TabInfo {
	t.Helper()
	rec := do(t, s, "POST", "/api/tabs", map[string]string{"path": rel})
	if rec.Code != 200 {
		t.Fatalf("open tab: %d %s", rec.Code, rec.Body.String())
	}
	return decode[TabInfo](t, rec)
}

func getFile(t *testing.T, s *Server, id, query string) fileResponse {
	t.Helper()
	rec := do(t, s, "GET", "/api/files/"+id+query, nil)
	if rec.Code != 200 {
		t.Fatalf("get file: %d %s", rec.Code, rec.Body.String())
	}
	return decode[fileResponse](t, rec)
}

func drain(ch chan []byte) []map[string]any {
	var out []map[string]any
	for {
		select {
		case b := <-ch:
			var m map[string]any
			json.Unmarshal(b, &m)
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestTabsStartEmptyAndOpenIsIdempotent(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "hi")
	var tabs struct{ Tabs []TabInfo }
	tabs = decode[struct{ Tabs []TabInfo }](t, do(t, s, "GET", "/api/tabs", nil))
	if len(tabs.Tabs) != 0 {
		t.Fatalf("expected no tabs at start, got %v", tabs.Tabs)
	}
	a := openTab(t, s, "a.txt")
	b := openTab(t, s, "a.txt")
	if a.FileId != b.FileId {
		t.Fatalf("reopening should reuse tab")
	}
	if a.AbsPath != filepath.Join(s.rootAbs, "a.txt") || a.Name != "a.txt" {
		t.Fatalf("bad tab info %+v", a)
	}
	tabs = decode[struct{ Tabs []TabInfo }](t, do(t, s, "GET", "/api/tabs", nil))
	if len(tabs.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs.Tabs))
	}
}

func TestCloseTabDoesNotDeleteFile(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "hi")
	a := openTab(t, s, "a.txt")
	do(t, s, "DELETE", "/api/tabs/"+a.FileId, nil)
	if _, err := os.Stat(s.abs("a.txt")); err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
	if rec := do(t, s, "GET", "/api/files/"+a.FileId, nil); rec.Code != 404 {
		t.Fatalf("closed tab id should 404, got %d", rec.Code)
	}
}

func TestNewFileNamingLowestUnusedN(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "new 1", "")
	writeFile(t, s, "new 3", "")
	got := decode[TabInfo](t, do(t, s, "POST", "/api/files", map[string]string{"parentPath": ""}))
	if got.Path != "new 2" {
		t.Fatalf("got %q want new 2", got.Path)
	}
	got = decode[TabInfo](t, do(t, s, "POST", "/api/files", map[string]string{"parentPath": ""}))
	if got.Path != "new 4" {
		t.Fatalf("got %q want new 4", got.Path)
	}
}

func TestEditHappyPathWritesToDisk(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "hello\nworld")
	registerClient(s, "c1")
	other := registerClient(s, "c2")
	a := openTab(t, s, "a.txt")
	f1 := getFile(t, s, a.FileId, "?cid=c1")
	getFile(t, s, a.FileId, "?cid=c2")

	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f1.VersionId, NewVersionId: "v2",
		From: Pos{0, 5}, To: Pos{0, 5}, Text: []string{"!"}, Removed: []string{""},
	})
	s.FlushAll()
	b, _ := os.ReadFile(s.abs("a.txt"))
	if string(b) != "hello!\nworld" {
		t.Fatalf("disk = %q", b)
	}
	var sawContent bool
	for _, m := range drain(other) {
		if m["type"] == "content" && m["content"] == "hello!\nworld" && m["versionId"] == "v2" {
			sawContent = true
		}
	}
	if !sawContent {
		t.Fatalf("other client did not receive content broadcast")
	}
}

func TestEditConflictRelocation(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "a\nb\nc")
	registerClient(s, "c1")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "?cid=c1")

	// another client prepends a line (v2), then c1 edits against v1
	s.HandleEdit("c2", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v2",
		From: Pos{0, 0}, To: Pos{0, 0}, Text: []string{"X", ""}, Removed: []string{""},
	})
	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v3",
		From: Pos{2, 1}, To: Pos{2, 1}, Text: []string{"!"}, Removed: []string{""},
	})
	s.FlushAll()
	b, _ := os.ReadFile(s.abs("a.txt"))
	if string(b) != "X\na\nb\nc!" {
		t.Fatalf("disk = %q", b)
	}
}

func TestExternalChangeSyncsAndEchoIsIgnored(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "one")
	registerClient(s, "c1")
	ch := registerClient(s, "c2")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "?cid=c2")

	// our own write must not produce a new version
	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v2",
		From: Pos{0, 3}, To: Pos{0, 3}, Text: []string{"!"}, Removed: []string{""},
	})
	drain(ch)
	s.FlushAll()
	s.syncOpenPath("a.txt")
	if msgs := drain(ch); len(msgs) != 0 {
		t.Fatalf("echo of own write should be ignored, got %v", msgs)
	}

	writeFile(t, s, "a.txt", "external")
	s.syncOpenPath("a.txt")
	msgs := drain(ch)
	if len(msgs) != 1 || msgs[0]["content"] != "external" {
		t.Fatalf("expected content broadcast, got %v", msgs)
	}
	if got := getFile(t, s, a.FileId, "?cid=c2"); got.Content != "external" || got.VersionId == "v2" {
		t.Fatalf("bad refetch %+v", got)
	}
}

func TestExternalDeleteClosesTab(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	a := openTab(t, s, "a.txt")
	os.Remove(s.abs("a.txt"))
	s.closeIfGone("a.txt")
	if rec := do(t, s, "GET", "/api/files/"+a.FileId, nil); rec.Code != 404 {
		t.Fatalf("tab should be closed, got %d", rec.Code)
	}
}

func TestLargeAndBinaryFiles(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "big.txt", strings.Repeat("a", maxDisplayBytes+1))
	writeFile(t, s, "bin.dat", "ab\x00cd")
	big := openTab(t, s, "big.txt")
	bin := openTab(t, s, "bin.dat")

	if r := getFile(t, s, big.FileId, ""); !r.TooLarge || r.Content != "" {
		t.Fatalf("expected tooLarge, got %+v", r.TooLarge)
	}
	if r := getFile(t, s, big.FileId, "?force=1"); r.TooLarge || len(r.Content) != maxDisplayBytes+1 {
		t.Fatalf("force should load content")
	}
	// the choice is per request: a later non-forced fetch is still gated
	if r := getFile(t, s, big.FileId, ""); !r.TooLarge {
		t.Fatalf("non-forced fetch should still be gated")
	}
	if r := getFile(t, s, bin.FileId, "?force=1"); !r.Binary || r.Content != "" {
		t.Fatalf("expected binary, got %+v", r)
	}
}

func TestLargeFileUnloadedWhenNoSubscribers(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "big.txt", strings.Repeat("a", maxDisplayBytes+1))
	registerClient(s, "c1")
	big := openTab(t, s, "big.txt")
	getFile(t, s, big.FileId, "?cid=c1&force=1")
	if !s.byId[big.FileId].loaded {
		t.Fatalf("should be loaded")
	}
	s.hub.OnUnsubscribe(s.hub.SetSubscription("c1", ""))
	if s.byId[big.FileId].loaded {
		t.Fatalf("should be unloaded once nobody has it")
	}
}

func TestDeleteAndUndo(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "d/a.txt", "A")
	writeFile(t, s, "b.txt", "B")
	a := openTab(t, s, "d/a.txt")
	b := openTab(t, s, "b.txt")

	rec := do(t, s, "DELETE", "/api/entries", map[string]string{"path": "d"})
	trashId := decode[map[string]string](t, rec)["trashId"]
	if _, err := os.Stat(s.abs("d")); !os.IsNotExist(err) {
		t.Fatalf("folder should be gone")
	}
	if rec := do(t, s, "GET", "/api/files/"+a.FileId, nil); rec.Code != 404 {
		t.Fatalf("tab under deleted folder should close")
	}

	rec = do(t, s, "POST", "/api/trash/"+trashId+"/restore", nil)
	if rec.Code != 200 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	if got := getFile(t, s, a.FileId, ""); got.Content != "A" {
		t.Fatalf("restored tab keeps its FileId and content")
	}
	tabs := decode[struct{ Tabs []TabInfo }](t, do(t, s, "GET", "/api/tabs", nil)).Tabs
	if len(tabs) != 2 || tabs[0].FileId != a.FileId || tabs[1].FileId != b.FileId {
		t.Fatalf("tab position not restored: %+v", tabs)
	}
}

func TestUndoFailsWhenPathOccupied(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "old")
	trashId := decode[map[string]string](t, do(t, s, "DELETE", "/api/entries", map[string]string{"path": "a.txt"}))["trashId"]
	writeFile(t, s, "a.txt", "new")
	if rec := do(t, s, "POST", "/api/trash/"+trashId+"/restore", nil); rec.Code != 409 {
		t.Fatalf("want 409, got %d", rec.Code)
	}
	b, _ := os.ReadFile(s.abs("a.txt"))
	if string(b) != "new" {
		t.Fatalf("occupying file must be untouched")
	}
}

func TestTrashExpires(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	trashId := decode[map[string]string](t, do(t, s, "DELETE", "/api/entries", map[string]string{"path": "a.txt"}))["trashId"]
	time.Sleep(trashTTL + 500*time.Millisecond)
	if rec := do(t, s, "POST", "/api/trash/"+trashId+"/restore", nil); rec.Code != 404 {
		t.Fatalf("want 404 after expiry, got %d", rec.Code)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.rootAbs, trashDirName)); len(entries) != 0 {
		t.Fatalf("trash dir should be empty, has %d", len(entries))
	}
}

func TestStartupWipesTrashDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, trashDirName, "leftover"), 0755)
	os.WriteFile(filepath.Join(dir, trashDirName, "leftover", "x"), []byte("x"), 0644)
	if _, err := NewServer(dir, embed.FS{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, trashDirName)); !os.IsNotExist(err) {
		t.Fatalf(".ntt-trash should be wiped on startup")
	}
}

func TestRenameAndMoveKeepFileId(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	os.MkdirAll(s.abs("sub"), 0755)
	a := openTab(t, s, "a.txt")

	if rec := do(t, s, "PUT", "/api/rename", map[string]string{"path": "a.txt", "name": "b.txt"}); rec.Code != 204 {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "PUT", "/api/move", map[string]string{"path": "b.txt", "newPath": "sub/b.txt"}); rec.Code != 204 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if got := getFile(t, s, a.FileId, ""); got.Path != "sub/b.txt" || got.Content != "x" {
		t.Fatalf("tab did not follow file: %+v", got)
	}

	// folder rename re-paths nested tabs
	do(t, s, "PUT", "/api/rename", map[string]string{"path": "sub", "name": "sub2"})
	if got := getFile(t, s, a.FileId, ""); got.Path != "sub2/b.txt" {
		t.Fatalf("got %q", got.Path)
	}
	// conflicts and cycles are rejected
	writeFile(t, s, "c.txt", "")
	if rec := do(t, s, "PUT", "/api/rename", map[string]string{"path": "c.txt", "name": "sub2"}); rec.Code != 409 {
		t.Fatalf("want 409, got %d", rec.Code)
	}
	if rec := do(t, s, "PUT", "/api/move", map[string]string{"path": "sub2", "newPath": "sub2/x"}); rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestPathValidationRejectsTraversalAndExcluded(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"../x", "a/../../x", "/etc/passwd", ".git/config", "node_modules/x", ".ntt-trash/x", ""} {
		if rec := do(t, s, "POST", "/api/tabs", map[string]string{"path": p}); rec.Code != 400 {
			t.Errorf("path %q: want 400, got %d", p, rec.Code)
		}
	}
}

func TestTreeOrderAndExclusions(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"b.txt", "A.txt", ".hidden", "zeta/x", "Alpha/x", ".git/config", "node_modules/m/x", "Beta/.git/x"} {
		writeFile(t, s, p, "")
	}
	tree := BuildTree(s.rootAbs)
	var folders, files []string
	for _, f := range tree.Folders {
		folders = append(folders, f.Name)
	}
	for _, f := range tree.Files {
		files = append(files, f.Name)
	}
	if strings.Join(folders, ",") != "Alpha,Beta,zeta" {
		t.Errorf("folders = %v", folders)
	}
	if strings.Join(files, ",") != ".hidden,A.txt,b.txt" {
		t.Errorf("files = %v", files)
	}
}

func TestDuplicateOpensTab(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	got := decode[TabInfo](t, do(t, s, "POST", "/api/duplicate", map[string]string{"path": "a.txt"}))
	if got.Path != "a (2).txt" {
		t.Fatalf("got %q", got.Path)
	}
}

func TestSearch(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "notes.txt", "line1\nfind the NEEDLE here\nline3")
	writeFile(t, s, "other.txt", "nothing")
	writeFile(t, s, "needle-name.txt", "no content match")
	writeFile(t, s, ".git/x", "needle")
	writeFile(t, s, "node_modules/x", "needle")

	rec := do(t, s, "GET", "/api/search?q=needle", nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	res := decode[[]SearchResult](t, rec)
	if len(res) != 2 || res[0].Path != "needle-name.txt" || res[1].Path != "notes.txt" {
		t.Fatalf("results = %+v", res)
	}
	if len(res[1].Sections) != 1 || res[1].Sections[0].StartLineNumber != 1 || !strings.Contains(res[1].Sections[0].Snippet, "NEEDLE") {
		t.Fatalf("sections = %+v", res[1].Sections)
	}
	if rec := do(t, s, "GET", "/api/search?q=(&regex=true", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad regex: want 400, got %d", rec.Code)
	}
}

func TestDownload(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.bin", "ab\x00cd")
	rec := do(t, s, "GET", "/api/download?path=a.bin", nil)
	if rec.Code != 200 || rec.Body.String() != "ab\x00cd" || !strings.Contains(rec.Header().Get("Content-Disposition"), "a.bin") {
		t.Fatalf("bad download: %d %q", rec.Code, rec.Header())
	}
}

func TestReorderTabsAndActive(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a", "")
	writeFile(t, s, "b", "")
	registerClient(s, "c1")
	a, b := openTab(t, s, "a"), openTab(t, s, "b")
	if rec := do(t, s, "PUT", "/api/tabs/order", map[string]any{"fileIds": []string{b.FileId, a.FileId}}); rec.Code != 204 {
		t.Fatalf("reorder: %d", rec.Code)
	}
	getFile(t, s, a.FileId, "?cid=c1")
	got := decode[struct {
		Tabs         []TabInfo
		ActiveFileId string
	}](t, do(t, s, "GET", "/api/tabs", nil))
	if got.Tabs[0].FileId != b.FileId || got.ActiveFileId != a.FileId {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteBehindFlushesAfterDelay(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "")
	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v2",
		From: Pos{0, 1}, To: Pos{0, 1}, Text: []string{"y"}, Removed: []string{""},
	})
	if b, _ := os.ReadFile(s.abs("a.txt")); string(b) != "x" {
		t.Fatalf("should not be written yet, got %q", b)
	}
	time.Sleep(flushDelay + 300*time.Millisecond)
	if b, _ := os.ReadFile(s.abs("a.txt")); string(b) != "xy" {
		t.Fatalf("disk = %q after delay", b)
	}
}

func TestRenameFlushesPendingEdit(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "")
	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v2",
		From: Pos{0, 1}, To: Pos{0, 1}, Text: []string{"y"}, Removed: []string{""},
	})
	do(t, s, "PUT", "/api/rename", map[string]string{"path": "a.txt", "name": "b.txt"})
	time.Sleep(flushDelay + 300*time.Millisecond)
	if b, _ := os.ReadFile(s.abs("b.txt")); string(b) != "xy" {
		t.Fatalf("b.txt = %q", b)
	}
	if _, err := os.Stat(s.abs("a.txt")); err == nil {
		t.Fatalf("a.txt was recreated by a late flush")
	}
}

func TestOutOfRangeEditSendsConflict(t *testing.T) {
	s := newTestServer(t)
	writeFile(t, s, "a.txt", "x")
	ch := registerClient(s, "c1")
	a := openTab(t, s, "a.txt")
	f := getFile(t, s, a.FileId, "?cid=c1")
	drain(ch)
	s.HandleEdit("c1", EditMessage{
		FileId: a.FileId, CurrentVersionId: f.VersionId, NewVersionId: "v2",
		From: Pos{9, 0}, To: Pos{9, 0}, Text: []string{"y"}, Removed: []string{""},
	})
	msgs := drain(ch)
	if len(msgs) != 1 || msgs[0]["type"] != "editConflict" {
		t.Fatalf("got %v", msgs)
	}
}

func TestCrossOriginMutationRefused(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("got %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "http://"+req.Host)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("same-origin got %d", rec.Code)
	}
}

func TestVersionStoreBoundedPerFile(t *testing.T) {
	st := newRecentVersionStore()
	for i := 0; i < versionsPerFile+10; i++ {
		st.Add("f", string(rune('a'+i)), "c")
	}
	if _, ok := st.Lookup("f", "a"); ok {
		t.Fatal("oldest should be evicted")
	}
	if _, ok := st.Lookup("f", string(rune('a'+versionsPerFile+9))); !ok {
		t.Fatal("newest should be kept")
	}
	st.Forget("f")
	if _, ok := st.Lookup("f", string(rune('a'+versionsPerFile+9))); ok {
		t.Fatal("forget failed")
	}
}
