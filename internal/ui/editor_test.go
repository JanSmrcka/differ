package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// editorModel is a real repo with one modified file and editor_cmd pointing at
// a script, so pressing e never depends on which editor the machine has.
func editorModel(t *testing.T) (Model, string) {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)

	stub := filepath.Join(t.TempDir(), "stubed")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.cfg.EditorCmd = stub + " {file}"
	return m, stub
}

// isQuit reports whether running cmd asks the program to exit. tea.ExecProcess
// returns an unexported message type, so a test cannot name what it wants —
// but it can name what it must not be.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// The whole point of #63: e used to set SelectedFile and return tea.Quit, so
// the editor replaced differ and differ was gone when it closed.
func TestEditor_PressingEDoesNotQuitDiffer(t *testing.T) {
	m, _ := editorModel(t)

	_, cmd := m.updateFileListMode(key("e"))
	if cmd == nil {
		t.Fatal("e should do something")
	}
	if isQuit(cmd) {
		t.Error("e must not quit differ")
	}
}

// e has always worked in the diff, and now in review too, but helpPairs only
// ever advertised it in the file list.
func TestEditor_TheDiffAndReviewHintsAdvertiseE(t *testing.T) {
	for _, mode := range []viewMode{modeDiff, modeReview} {
		m := Model{mode: mode}
		found := false
		for _, p := range m.helpPairs() {
			if p.key == "e" {
				found = true
			}
		}
		if !found {
			t.Errorf("mode %v does not advertise e", mode)
		}
	}
}

// An editor that cannot run must leave differ alive and say why.
func TestEditor_AFailureToResolveIsReportedAndDifferSurvives(t *testing.T) {
	m, _ := editorModel(t)
	m.cfg.EditorCmd = "definitely-not-a-real-binary {file}"

	_, cmd := m.updateFileListMode(key("e"))
	updated, next := m.Update(cmd())
	m = updated.(Model)

	if isQuit(next) {
		t.Error("a broken editor must not quit differ")
	}
	if !strings.Contains(m.statusMsg, "definitely-not-a-real-binary") {
		t.Errorf("statusMsg = %q, want it to name the missing editor", m.statusMsg)
	}
}

// Coming back from the editor must refresh, since the file may have changed,
// without throwing away where the reviewer was.
func TestEditor_ReturningFromTheEditorRefreshesAndKeepsThePosition(t *testing.T) {
	m, _ := editorModel(t)
	u, _ := m.updateFileListMode(key("l"))
	m = u.(Model)
	m = press(t, m, "j", "j")
	cursor, diffCursor := m.cursor, m.diffCursor

	updated, cmd := m.Update(editorDoneMsg{})
	m = updated.(Model)

	if cmd == nil {
		t.Error("returning from the editor should refresh")
	}
	if m.cursor != cursor || m.diffCursor != diffCursor {
		t.Errorf("position moved: cursor %d→%d, diffCursor %d→%d",
			cursor, m.cursor, diffCursor, m.diffCursor)
	}
}

// e in the diff means the same thing as e in the file list.
func TestEditor_EWorksInTheDiffToo(t *testing.T) {
	m, _ := editorModel(t)
	u, _ := m.updateFileListMode(key("l"))
	m = u.(Model)

	_, cmd := m.updateDiffMode(key("e"))
	if cmd == nil || isQuit(cmd) {
		t.Error("e in the diff should open the editor without quitting")
	}
}

// differ commit builds the same Model but never read SelectedFile, so e there
// used to quit and do nothing at all.
func TestEditor_EInCommitModeNoLongerQuitsSilently(t *testing.T) {
	m, _ := editorModel(t)
	m.StartInCommitMode()
	m.mode = modeFileList

	_, cmd := m.updateFileListMode(key("e"))
	if cmd == nil || isQuit(cmd) {
		t.Error("e should open the editor in differ commit too")
	}
}

// The line handed to the editor is a line in the file on disk. A removed line
// has none, so the cursor's nearest preceding on-disk line is used — that
// lands the editor just above the code being read.
func TestEditor_LineForARemovedLineUsesTheNearestLineThatExists(t *testing.T) {
	m, _ := editorModel(t)
	u, _ := m.updateFileListMode(key("l"))
	m = u.(Model)

	parsed := m.renderer.Parsed()
	removed := -1
	for i, l := range parsed.Lines {
		if l.Type == LineRemoved {
			removed = i
			break
		}
	}
	if removed < 0 {
		t.Skip("fixture has no removed line")
	}
	m = m.setCursor(removed)
	if got := parsed.Lines[m.diffCursor].NewNum; got > 0 {
		t.Fatalf("cursor did not land on a removed line, NewNum = %d", got)
	}

	line := m.editorLine()
	if line <= 0 {
		t.Fatalf("editorLine = %d, want a real line", line)
	}
	// It must be a line that actually exists on the new side.
	found := false
	for _, l := range parsed.Lines {
		if l.NewNum == line {
			found = true
		}
	}
	if !found {
		t.Errorf("editorLine = %d, which is not a line of the new file", line)
	}
}

// From the file list there is no line to aim at.
func TestEditor_NoLineIsClaimedFromTheFileList(t *testing.T) {
	m, _ := editorModel(t)
	if got := m.editorLine(); got != 0 {
		t.Errorf("editorLine = %d in the file list, want 0", got)
	}
}
