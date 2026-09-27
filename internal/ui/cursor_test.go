package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
)

// diffModel returns a model sitting in diff view on a loaded multi-hunk diff.
func diffModel(t *testing.T, fixture string, height int) Model {
	t.Helper()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "src.ts", Status: git.StatusModified}}})
	m.mode = modeDiff
	m.ready = true
	m.viewport = viewport.New(80, height)
	parsed := ParseDiff(testutil.Fixture(t, fixture).Diff)
	r := NewDiffRenderer(parsed, "src.ts", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: true})
	return updated.(Model)
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	return tea.KeyMsg{Type: tea.KeyDown}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		updated, _ := m.updateDiffMode(key(k))
		m = updated.(Model)
	}
	return m
}

func TestDiffCursor_StartsOnFirstCommentableLine(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	want := m.renderer.Parsed().FirstCommentableLine()
	if m.diffCursor != want {
		t.Errorf("diffCursor = %d, want %d", m.diffCursor, want)
	}
	if m.renderer.Parsed().Lines[m.diffCursor].Type == LineHunkHeader {
		t.Error("cursor should not start on a hunk header")
	}
}

func TestDiffCursor_JKMoveByLine(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	start := m.diffCursor

	m = press(t, m, "j", "j")
	if m.diffCursor != start+2 {
		t.Errorf("after jj cursor = %d, want %d", m.diffCursor, start+2)
	}
	m = press(t, m, "k")
	if m.diffCursor != start+1 {
		t.Errorf("after k cursor = %d, want %d", m.diffCursor, start+1)
	}
}

func TestDiffCursor_ClampsAtBothEnds(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)

	for i := 0; i < 100; i++ {
		m = press(t, m, "k")
	}
	if m.diffCursor != 0 {
		t.Errorf("cursor ran above the top: %d", m.diffCursor)
	}

	for i := 0; i < 500; i++ {
		m = press(t, m, "j")
	}
	if want := m.renderer.LineCount() - 1; m.diffCursor != want {
		t.Errorf("cursor = %d, want last line %d", m.diffCursor, want)
	}
}

func TestDiffCursor_GAndShiftGJumpToEnds(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)

	m = press(t, m, "G")
	if want := m.renderer.LineCount() - 1; m.diffCursor != want {
		t.Errorf("after G cursor = %d, want %d", m.diffCursor, want)
	}
	m = press(t, m, "g")
	if m.diffCursor != 0 {
		t.Errorf("after g cursor = %d, want 0", m.diffCursor)
	}
}

func TestDiffCursor_ViewportFollowsCursorDown(t *testing.T) {
	// Height 5 guarantees the cursor leaves the window well before the end.
	m := diffModel(t, "multi_hunk", 5)
	if m.viewport.YOffset != 0 {
		t.Fatalf("expected to start at the top, YOffset = %d", m.viewport.YOffset)
	}

	m = press(t, m, "G")

	if m.diffCursor < m.viewport.YOffset || m.diffCursor >= m.viewport.YOffset+m.viewport.Height {
		t.Errorf("cursor %d is outside the viewport window [%d,%d)", m.diffCursor, m.viewport.YOffset, m.viewport.YOffset+m.viewport.Height)
	}
}

func TestDiffCursor_ViewportFollowsCursorBackUp(t *testing.T) {
	m := diffModel(t, "multi_hunk", 5)
	m = press(t, m, "G")
	m = press(t, m, "g")

	if m.viewport.YOffset != 0 {
		t.Errorf("after g, YOffset = %d, want 0", m.viewport.YOffset)
	}
}

func TestDiffCursor_HunkNavigation(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	parsed := m.renderer.Parsed()

	m = press(t, m, "}")
	if want := parsed.Hunks[1].StartLine + 1; m.diffCursor != want {
		t.Errorf("after } cursor = %d, want %d", m.diffCursor, want)
	}

	m = press(t, m, "{")
	if want := parsed.Hunks[0].StartLine + 1; m.diffCursor != want {
		t.Errorf("after { cursor = %d, want %d", m.diffCursor, want)
	}
}

func TestDiffCursor_HunkNavigationStopsAtTheEnds(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)

	m = press(t, m, "{") // already in the first hunk
	if want := m.renderer.Parsed().Hunks[0].StartLine + 1; m.diffCursor != want {
		t.Errorf("{ in the first hunk moved the cursor to %d", m.diffCursor)
	}

	m = press(t, m, "}", "}", "}")
	if want := m.renderer.Parsed().Hunks[1].StartLine + 1; m.diffCursor != want {
		t.Errorf("} past the last hunk moved the cursor to %d, want %d", m.diffCursor, want)
	}
}

func TestDiffCursor_MarkerIsVisibleInTheViewport(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	if !strings.Contains(m.viewport.View(), cursorMarker) {
		t.Error("viewport content has no cursor marker")
	}
}

func TestDiffCursor_RefreshKeepsCursorPosition(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = press(t, m, "j", "j", "j")
	before := m.diffCursor

	// A poll-triggered reload of the same diff must not move the cursor.
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: false})
	m = updated.(Model)

	if m.diffCursor != before {
		t.Errorf("refresh moved the cursor from %d to %d", before, m.diffCursor)
	}
}

func TestDiffCursor_ShrinkingDiffClampsCursor(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = press(t, m, "G")

	// The file changed underneath: a much shorter diff arrives.
	parsed := ParseDiff(testutil.Fixture(t, "tabs_indent").Diff)
	r := NewDiffRenderer(parsed, "tabs.go", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: false})
	m = updated.(Model)

	if m.diffCursor >= m.renderer.LineCount() {
		t.Errorf("cursor %d is past the end of the new diff (%d lines)", m.diffCursor, m.renderer.LineCount())
	}
}
