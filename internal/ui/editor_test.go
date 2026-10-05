package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/editor"
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
	// Pin the environment: otherwise these tests would take the tmux path
	// when the suite happens to run inside tmux, and the inline one on CI.
	m.editorEnv = editor.Env{Editor: stub}
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

// e has always worked in the diff and in review, but the old hand-written
// hint list only ever advertised it in the file list. It is not compact enough
// to earn a place in the command bar, so the keymap is where it has to appear.
func TestEditor_TheDiffAndReviewKeymapsDocumentE(t *testing.T) {
	t.Parallel()
	for _, mode := range []viewMode{modeFileList, modeDiff} {
		found := false
		for _, b := range keymapFor(mode) {
			for _, k := range b.Keys {
				if k == "e" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("mode %v does not document e", mode)
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

	updated, cmd := m.Update(editorDoneMsg{reload: true})
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

// The renderer is loaded asynchronously, so between switching files and the
// diff arriving it still belongs to the previous one. Gating on the mode is
// not enough: entering the diff and n/p both leave the old renderer in place.
func TestEditor_NoLineIsClaimedWhileTheRendererBelongsToAnotherFile(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	tr.Write("second.ts", "const a = 1\nconst b = 2\nconst c = 3\n")
	tr.Stage("second.ts")
	m := liveModel(t, tr)
	if len(m.files) < 2 {
		t.Skipf("need two files, got %d", len(m.files))
	}

	u, _ := m.updateFileListMode(key("l"))
	m = u.(Model)
	m = m.setCursor(m.renderer.Parsed().FirstCommentableLine())
	if m.editorLine() == 0 {
		t.Fatal("the diff should give a line before the file is switched")
	}

	// n moves to the next file; its diff has not arrived yet, so the renderer
	// still describes the old one.
	u, _ = m.updateDiffMode(key("J"))
	m = u.(Model)

	if got := m.editorLine(); got != 0 {
		t.Errorf("editorLine = %d while the renderer still belongs to %q, want 0",
			got, m.files[0].change.Path)
	}
}

// A hunk header has no line on either side. Scanning backwards from it walks
// into the previous hunk and returns its last line, which can be a hundred
// lines from what the reviewer is looking at.
func TestEditor_ACursorOnAHunkHeaderUsesThatHunksStart(t *testing.T) {
	m, _ := editorModel(t)
	u, _ := m.updateFileListMode(key("l"))
	m = u.(Model)

	parsed := m.renderer.Parsed()
	// The second hunk's header: the first one has nothing before it, which is
	// the case the HunkAt fallback already covers.
	header := -1
	seen := 0
	for i, l := range parsed.Lines {
		if l.Type == LineHunkHeader {
			seen++
			if seen == 2 {
				header = i
				break
			}
		}
	}
	if header < 0 {
		t.Skip("fixture has fewer than two hunks")
	}
	m = m.setCursor(header)
	if m.diffCursor != header {
		t.Skipf("the cursor does not rest on a hunk header (landed on %d)", m.diffCursor)
	}

	hunk, ok := parsed.HunkAt(header)
	if !ok {
		t.Fatal("no hunk at its own header")
	}
	if got := m.editorLine(); got != hunk.NewStart {
		t.Errorf("editorLine = %d, want the hunk's own start %d", got, hunk.NewStart)
	}
}

// A detached plan opens the file elsewhere, so it runs in an ordinary tea.Cmd
// rather than taking the terminal — and a failure there still leaves differ
// running.
func TestEditor_ADetachedPlanReportsItsFailureWithoutQuitting(t *testing.T) {
	m, _ := editorModel(t)

	// A window strategy outside tmux is refused by Resolve, which is the
	// detached path's error arriving through the same message.
	m.cfg.EditorStrategy = "window"
	_, cmd := m.updateFileListMode(key("e"))
	updated, next := m.Update(cmd())
	m = updated.(Model)

	if isQuit(next) {
		t.Error("a refused strategy must not quit differ")
	}
	if !strings.Contains(m.statusMsg, "tmux") {
		t.Errorf("statusMsg = %q, want it to explain the tmux requirement", m.statusMsg)
	}
}

// Opening the file somewhere else does not mean it changed, so there is
// nothing to reload — the two-second poll covers it if it does.
func TestEditor_ADetachedOpenSaysWhereItWentAndDoesNotReload(t *testing.T) {
	m, _ := editorModel(t)

	updated, cmd := m.Update(editorDoneMsg{desc: "opened src.ts in nvim (differ:1.1)"})
	m = updated.(Model)

	if cmd != nil {
		t.Error("a detached open should not force a reload")
	}
	if !strings.Contains(m.statusMsg, "differ:1.1") {
		t.Errorf("statusMsg = %q, want it to name where the file went", m.statusMsg)
	}
}
