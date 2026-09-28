package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The right panel prepends a label and a blank row, so the viewport gets
// listHeight, not contentHeight — otherwise the bottom rows are clipped while
// scrollToCursor still counts them as visible.
func TestViewport_MatchesTheRoomThePanelActuallyHas(t *testing.T) {
	m := chromeModel(t, 120, 30)
	if got, want := m.viewport.Height, m.listHeight(); got != want {
		t.Errorf("viewport height %d, want listHeight %d (contentHeight is %d)", got, want, m.contentHeight())
	}
}

// With the cursor on the last line it must be on screen.
func TestViewport_LastLineIsVisibleAfterG(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	m = u.(Model)
	if cmd := m.loadDiffCmd(true); cmd != nil {
		u, _ = m.Update(cmd())
		m = u.(Model)
	}
	m.mode = modeDiff
	m = press(t, m, "G")

	row, ok := m.renderer.RowFor(m.diffCursor)
	if !ok {
		t.Fatal("no row for the cursor")
	}
	if row < m.viewport.YOffset || row >= m.viewport.YOffset+m.viewport.Height {
		t.Fatalf("cursor row %d outside the window [%d,%d)", row, m.viewport.YOffset, m.viewport.YOffset+m.viewport.Height)
	}
	// And it must actually be painted.
	if !strings.Contains(m.View(), cursorMarker) {
		t.Errorf("the cursor is not on screen:\n%s", m.View())
	}
}

// statusMsg must stay reachable when an input replaces the hint bar.
func TestStatusMsg_VisibleInCommitMode(t *testing.T) {
	m := chromeModel(t, 120, 30)
	m.mode = modeCommit
	m.statusMsg = "commit message is empty"

	if view := m.View(); !strings.Contains(view, "commit message is empty") {
		t.Errorf("status is unreachable in commit mode:\n%s", view)
	}
}

func TestStatusMsg_VisibleWhileTheCommentEditorIsOpen(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = u.(Model)
	u, _ = m.Update(key("r"))
	m = u.(Model)
	u, _ = m.updateReviewMode(key("c"))
	m = u.(Model)

	// Saving an empty comment must say so rather than silently doing nothing.
	u, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = u.(Model)

	if m.statusMsg == "" {
		t.Fatal("precondition: an empty save should set a status message")
	}
	if view := m.View(); !strings.Contains(view, m.statusMsg) {
		t.Errorf("status %q is unreachable while the editor is open:\n%s", m.statusMsg, view)
	}
}

func TestStatusMsg_VisibleWhileCreatingABranch(t *testing.T) {
	m := chromeModel(t, 120, 30)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.statusMsg = "empty branch name"

	if view := m.View(); !strings.Contains(view, "empty branch name") {
		t.Errorf("status is unreachable while creating a branch:\n%s", view)
	}
}

// The refit must also run for async messages that change the footer.
func TestViewport_RefitAfterAsyncStatusChange(t *testing.T) {
	m := chromeModel(t, 100, 20)
	m.mode = modeDiff

	// A push result arrives; it adds a status row to the footer.
	u, _ := m.Update(pushDoneMsg{})
	m = u.(Model)

	if got, want := m.viewport.Height, m.listHeight(); got != want {
		t.Errorf("after an async status change the viewport is %d, want %d", got, want)
	}
}

// diffWidth must describe the frame that exists, not the old bordered cards.
func TestDiffWidth_MatchesTheRenderedPanel(t *testing.T) {
	m := chromeModel(t, 120, 30)

	// Measure the real right-hand panel from a rendered row.
	rows := strings.Split(m.View(), "\n")
	var measured int
	for _, r := range rows {
		i := strings.Index(r, verticalDivider)
		if i < 0 {
			continue
		}
		if w := lipglossWidth(r[i:]) - 1 - panelGap; w > measured {
			measured = w
		}
	}
	if measured == 0 {
		t.Fatal("could not find a divider row to measure")
	}
	if m.diffWidth() != measured {
		t.Errorf("diffWidth() = %d but the rendered panel is %d columns", m.diffWidth(), measured)
	}
}
