package backend

import (
	"log"
	"os"
	"slices"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Pos mirrors a CodeMirror 5 {line, ch} document position.
type Pos struct {
	Line int `json:"line"`
	Ch   int `json:"ch"`
}

// EditMessage mirrors a CodeMirror 5 change event, sent once per keystroke over the
// "edit" WebSocket message. From/To is the replaced range as of CurrentVersionId;
// Text is the new lines, Removed is the lines that were there before.
type EditMessage struct {
	Type             string   `json:"type"`
	FileId           string   `json:"fileId"`
	CurrentVersionId string   `json:"currentVersionId"`
	NewVersionId     string   `json:"newVersionId"`
	From             Pos      `json:"from"`
	To               Pos      `json:"to"`
	Text             []string `json:"text"`
	Removed          []string `json:"removed"`
}

// extractRange returns the lines lying between from and to (CodeMirror's "removed"
// semantics: a partial first/last line, full lines in between).
func extractRange(lines []string, from, to Pos) ([]string, bool) {
	if from.Line < 0 || to.Line < from.Line || to.Line >= len(lines) {
		return nil, false
	}
	firstRunes := []rune(lines[from.Line])
	lastRunes := []rune(lines[to.Line])
	if from.Ch < 0 || from.Ch > len(firstRunes) || to.Ch < 0 || to.Ch > len(lastRunes) {
		return nil, false
	}
	if from.Line == to.Line {
		if from.Ch > to.Ch {
			return nil, false
		}
		return []string{string(firstRunes[from.Ch:to.Ch])}, true
	}
	result := make([]string, 0, to.Line-from.Line+1)
	result = append(result, string(firstRunes[from.Ch:]))
	for i := from.Line + 1; i < to.Line; i++ {
		result = append(result, lines[i])
	}
	result = append(result, string(lastRunes[:to.Ch]))
	return result, true
}

// replaceRange applies a CodeMirror-style change (replace from..to with text) to a
// slice of lines, returning the resulting slice.
func replaceRange(lines []string, from, to Pos, text []string) ([]string, bool) {
	if from.Line < 0 || to.Line < from.Line || to.Line >= len(lines) {
		return nil, false
	}
	firstRunes := []rune(lines[from.Line])
	lastRunes := []rune(lines[to.Line])
	if from.Ch < 0 || from.Ch > len(firstRunes) || to.Ch < 0 || to.Ch > len(lastRunes) {
		return nil, false
	}
	prefix := string(firstRunes[:from.Ch])
	suffix := string(lastRunes[to.Ch:])

	var replacement []string
	if len(text) == 1 {
		replacement = []string{prefix + text[0] + suffix}
	} else {
		replacement = make([]string, len(text))
		replacement[0] = prefix + text[0]
		copy(replacement[1:len(text)-1], text[1:len(text)-1])
		replacement[len(text)-1] = text[len(text)-1] + suffix
	}

	result := make([]string, 0, len(lines)-(to.Line-from.Line+1)+len(replacement))
	result = append(result, lines[:from.Line]...)
	result = append(result, replacement...)
	result = append(result, lines[to.Line+1:]...)
	return result, true
}

// buildLineMapping line-diffs oldContent against newContent and returns, for every
// old line number that falls inside an unmodified hunk, its corresponding line number
// in newContent. equalLine reports which old line numbers are unmodified.
//
// A trailing "\n" is appended to both inputs before diffing so diffmatchpatch's
// line-splitter always produces exactly len(strings.Split(content, "\n")) chunks,
// keeping its line numbering aligned with ours (which counts a trailing newline as
// introducing one more, empty, final line).
func buildLineMapping(oldContent, newContent string) (oldToNew map[int]int, equalLine map[int]bool) {
	dmp := diffmatchpatch.New()
	t1, t2, _ := dmp.DiffLinesToChars(oldContent+"\n", newContent+"\n")
	diffs := dmp.DiffMain(t1, t2, false)

	oldToNew = map[int]int{}
	equalLine = map[int]bool{}
	oldLine, newLine := 0, 0
	for _, d := range diffs {
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			for i := 0; i < n; i++ {
				oldToNew[oldLine+i] = newLine + i
				equalLine[oldLine+i] = true
			}
			oldLine += n
			newLine += n
		case diffmatchpatch.DiffDelete:
			oldLine += n
		case diffmatchpatch.DiffInsert:
			newLine += n
		}
	}
	return
}

// relocateAndApply maps msg's from/to onto latestContent's line numbering (via the
// snapshot->latest line diff) and applies removed/text there. It fails if any line
// spanned by from..to falls inside a hunk that changed between snapshot and latest.
func relocateAndApply(snapshotContent, latestContent string, msg EditMessage) (string, bool) {
	oldToNew, equalLine := buildLineMapping(snapshotContent, latestContent)
	for l := msg.From.Line; l <= msg.To.Line; l++ {
		if !equalLine[l] {
			return "", false
		}
	}
	newFrom := Pos{Line: oldToNew[msg.From.Line], Ch: msg.From.Ch}
	newTo := Pos{Line: oldToNew[msg.To.Line], Ch: msg.To.Ch}
	latestLines := strings.Split(latestContent, "\n")
	mergedLines, ok := replaceRange(latestLines, newFrom, newTo, msg.Text)
	if !ok {
		return "", false
	}
	return strings.Join(mergedLines, "\n"), true
}

// HandleEdit processes a client's "edit" WS message: applies it directly if the
// client was up to date, otherwise attempts to relocate it against whatever the
// latest content turned out to be. senderCid is whichever connection sent msg.
func (s *Server) HandleEdit(senderCid string, msg EditMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.byId[msg.FileId]
	if f == nil || !f.loaded {
		return
	}

	// Always snapshot the version about to be superseded so a racing client citing
	// it as CurrentVersionId can still find something to resolve against.
	s.hub.Versions.Add(f.id, f.versionId, f.content)

	if msg.CurrentVersionId == f.versionId {
		s.applyHappyPathEditLocked(senderCid, msg, f)
		return
	}
	s.applyConflictEditLocked(senderCid, msg, f)
}

// commitLocked sets f's content/version and writes it straight to disk.
func (s *Server) commitLocked(f *openFile, content, versionId string) bool {
	if err := os.WriteFile(s.abs(f.path), []byte(content), 0644); err != nil {
		log.Printf("[edit] disk write error: %v", err)
		return false
	}
	f.content = content
	f.versionId = versionId
	s.hub.Versions.Add(f.id, versionId, content)
	return true
}

func (s *Server) applyHappyPathEditLocked(senderCid string, msg EditMessage, f *openFile) {
	lines := strings.Split(f.content, "\n")
	newLines, ok := replaceRange(lines, msg.From, msg.To, msg.Text)
	if !ok {
		log.Printf("[edit] out-of-range edit fileId=%s from=%+v to=%+v", msg.FileId, msg.From, msg.To)
		return
	}
	newContent := strings.Join(newLines, "\n")
	if !s.commitLocked(f, newContent, msg.NewVersionId) {
		return
	}
	s.hub.BroadcastContent(f.id, newContent, msg.NewVersionId, senderCid)
}

func (s *Server) applyConflictEditLocked(senderCid string, msg EditMessage, f *openFile) {
	snapshot, found := s.hub.Versions.Lookup(msg.FileId, msg.CurrentVersionId)
	if !found {
		s.hub.SendEditConflict(senderCid, f.id, f.content, f.versionId)
		return
	}

	snapshotLines := strings.Split(snapshot, "\n")
	removedActual, ok := extractRange(snapshotLines, msg.From, msg.To)
	if !ok || !slices.Equal(removedActual, msg.Removed) {
		s.hub.SendEditConflict(senderCid, f.id, f.content, f.versionId)
		return
	}

	naiveLines, ok := replaceRange(snapshotLines, msg.From, msg.To, msg.Text)
	if !ok {
		s.hub.SendEditConflict(senderCid, f.id, f.content, f.versionId)
		return
	}
	naiveContent := strings.Join(naiveLines, "\n")

	merged, ok := relocateAndApply(snapshot, f.content, msg)
	if !ok {
		s.hub.SendEditConflict(senderCid, f.id, f.content, f.versionId)
		return
	}

	freshVersionId := uniqueId(5)
	if !s.commitLocked(f, merged, freshVersionId) {
		return
	}
	// Also cache the sender's naive (non-merged) result under its own NewVersionId,
	// since that's what the client's local editor actually contains and its next
	// chained edit will cite this as CurrentVersionId.
	s.hub.Versions.Add(f.id, msg.NewVersionId, naiveContent)
	s.hub.BroadcastContent(f.id, merged, freshVersionId, "")
}

// syncFromDiskLocked folds an external change to f's file into the same
// versioned stream as a client edit, then broadcasts it to all subscribers.
func (s *Server) syncFromDiskLocked(f *openFile) {
	if !f.loaded {
		return
	}
	b, err := os.ReadFile(s.abs(f.path))
	if err != nil {
		return
	}
	content := string(b)
	if content == f.content {
		return // our own write echoing back, or a no-op touch
	}
	s.hub.Versions.Add(f.id, f.versionId, f.content)
	f.content = content
	f.versionId = uniqueId(5)
	s.hub.Versions.Add(f.id, f.versionId, content)
	s.hub.BroadcastContent(f.id, content, f.versionId, "")
}
