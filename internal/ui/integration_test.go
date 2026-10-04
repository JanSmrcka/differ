package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// liveModel wires a Model to a real git repo the way cmd/root.go does, so the
// whole path — git → parse → render → viewport → View — is exercised.
func liveModel(t *testing.T, tr *testutil.Repo) Model {
	t.Helper()
	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := repo.ChangedFiles(false, "")
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := repo.UntrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	th := theme.Themes["dark"]
	m := NewModel(repo, config.Default(), changes, untracked, NewStyles(th), th, false, "")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m = updated.(Model)

	// Run the diff load command synchronously and feed the result back.
	if cmd := m.loadDiffCmd(true); cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestIntegration_DiffViewShowsCursorOnRealRepo(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	m := liveModel(t, tr)
	m.mode = modeDiff

	view := m.View()
	if !strings.Contains(view, cursorMarker) {
		t.Errorf("rendered view has no cursor marker:\n%s", view)
	}
	if !strings.Contains(view, "await getUser(id)") {
		t.Errorf("rendered view is missing the changed line:\n%s", view)
	}
	t.Logf("\n%s", view)
}

func TestIntegration_CursorNavigatesHunksOnRealRepo(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	m := liveModel(t, tr)
	m.mode = modeDiff
	if m.renderer == nil {
		t.Fatal("no renderer after loading a real diff")
	}

	start := m.diffCursor
	updated, _ := m.updateDiffMode(key("}"))
	m = updated.(Model)

	if m.diffCursor == start {
		t.Fatal("} did not move the cursor between hunks")
	}
	addr, ok := m.cursorAddress()
	if !ok {
		t.Fatal("cursor has no address")
	}
	if addr.HunkIndex != 1 {
		t.Errorf("after }, HunkIndex = %d, want 1", addr.HunkIndex)
	}
}

func TestIntegration_UntrackedFileIsReviewable(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "x\n", "init")
	tr.Untracked("src/utils/format.ts", "export const fmt = (s: string) => s.trim()\nexport const n = 1\n")

	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	th := theme.Themes["dark"]
	untracked, _ := repo.UntrackedFiles()
	m := NewModel(repo, config.Default(), nil, untracked, NewStyles(th), th, false, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m = updated.(Model)
	if cmd := m.loadDiffCmd(true); cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}

	if m.renderer == nil {
		t.Fatal("untracked file produced no renderer")
	}
	addr, ok := m.cursorAddress()
	if !ok {
		t.Fatal("untracked file cursor has no address")
	}
	if addr.NewLine != 1 {
		t.Errorf("cursor NewLine = %d, want 1", addr.NewLine)
	}
	if !strings.Contains(m.viewport.View(), "trim()") {
		t.Error("untracked file content not rendered")
	}
}

func TestIntegration_SplitViewKeepsCursorAddress(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	m := liveModel(t, tr)
	m.mode = modeDiff
	updated, _ := m.updateDiffMode(key("}"))
	m = updated.(Model)
	before, _ := m.cursorAddress()

	// Toggle to split and reload the way the key handler does.
	m.splitDiff = true
	if cmd := m.loadDiffCmd(false); cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}

	after, ok := m.cursorAddress()
	if !ok {
		t.Fatal("cursor lost its address after switching to split")
	}
	if before != after {
		t.Errorf("split view changed the cursor address:\n before %+v\n after  %+v", before, after)
	}
}

func TestIntegration_ReviewModeRendersOnRealRepo(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.AgentChangeset()

	m := liveModel(t, tr)
	updated, cmd := m.updateFileListMode(key("enter"))
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}

	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	view := m.View()
	if !strings.Contains(view, "review") {
		t.Errorf("view does not indicate review mode:\n%s", view)
	}
	t.Logf("\n%s", view)
}

func TestIntegration_WriteACommentAndSeeItInTheDiff(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	// Put the cursor on the changed line and write a comment.
	m = m.setCursor(lineIndexOf(t, m.renderer.Parsed(), LineAdded, "  const user = await getUser(id)"))
	updated, _ = m.updateDiffMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "keep this awaited")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "keep this awaited") {
		t.Errorf("comment body is not on screen:\n%s", view)
	}
	// With the cursor still on the commented line the cursor marker wins, so
	// move away to see the comment's own gutter marker.
	moved := m.moveCursor(2)
	if !strings.Contains(moved.View(), commentMarker) {
		t.Errorf("no comment gutter marker on screen:\n%s", moved.View())
	}
	if !strings.Contains(view, "1 comment") {
		t.Errorf("status bar does not report the comment:\n%s", view)
	}

	// The working tree must be untouched by all of that.
	if got := strings.Join(tr.Status(), "\n"); !strings.Contains(got, "src.ts") || len(tr.Status()) != 1 {
		t.Errorf("git state changed: %v", tr.Status())
	}
	t.Logf("\n%s", view)
}

func TestIntegration_CommentEditorBarRenders(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)
	m = m.setCursor(lineIndexOf(t, m.renderer.Parsed(), LineAdded, "  const user = await getUser(id)"))
	updated, _ = m.updateDiffMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "needs await\nsecond line")

	view := m.View()
	if !strings.Contains(view, "needs await") {
		t.Errorf("editor content missing:\n%s", view)
	}
	t.Logf("\n%s", view)
}

func TestIntegration_GoFileWithTabsRendersInsideThePanel(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "tabs_indent"))

	m := liveModel(t, tr)
	m.mode = modeDiff

	view := m.View()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d columns, terminal is %d", i, w, m.width)
		}
	}
	t.Logf("\n%s", view)
}
