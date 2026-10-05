package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// #2: a binary (or empty) diff must replace what is on screen, not leave the
// previous file's diff behind.
func TestDiffLoaded_BinaryFileReplacesThePreviousDiff(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	if !strings.Contains(m.viewport.View(), "getUser") {
		t.Fatal("precondition: the text diff should be on screen")
	}

	binary := ParseDiff(testutil.Fixture(t, "binary_file").Diff)
	r := NewDiffRenderer(binary, "img.png", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: true})
	m = updated.(Model)

	view := m.viewport.View()
	if strings.Contains(view, "getUser") {
		t.Errorf("previous file's diff is still on screen:\n%s", view)
	}
	if !strings.Contains(view, "Binary file") {
		t.Errorf("binary placeholder not rendered:\n%s", view)
	}
}

func TestDiffLoaded_EmptyDiffClearsTheViewport(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)

	empty := NewDiffRenderer(ParseNewFile(""), "empty.txt", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: empty, index: 0, resetScroll: true})
	m = updated.(Model)

	if strings.Contains(m.viewport.View(), "getUser") {
		t.Errorf("previous diff still on screen:\n%s", m.viewport.View())
	}
}

// #5: resizing must not send the reviewer back to the top of the diff.
func TestResize_PreservesCursorPosition(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	m.mode = modeDiff
	m = press(t, m, "j", "j", "j")
	before := m.diffCursor
	if before == 0 {
		t.Fatal("precondition: cursor should have moved")
	}

	updated, cmd := m.handleResize(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)
	// The reload the resize triggers must not reset the cursor either.
	if cmd != nil {
		if msg, ok := cmd().(diffLoadedMsg); ok {
			updated, _ = m.handleDiffLoaded(msg)
			m = updated.(Model)
		}
	}

	if m.diffCursor != before {
		t.Errorf("resize moved the cursor from %d to %d", before, m.diffCursor)
	}
}

// On the very first load there is no position to preserve, so the cursor
// should still land on the first reviewable line.
func TestFirstLoad_PlacesCursorEvenWithoutReset(t *testing.T) {
	m := newTestModel(t, nil)
	m.ready = true
	m.width, m.height = 120, 30
	updated, _ := m.handleResize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", m.styles, m.theme, 80)
	updated, _ = m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: false})
	m = updated.(Model)

	want := parsed.FirstCommentableLine()
	if m.diffCursor != want {
		t.Errorf("first load put the cursor at %d, want %d", m.diffCursor, want)
	}
}

// #6: a background poll that produces identical content must not yank the
// viewport back to the cursor after the user scrolled with pgdn/mouse.
func TestBackgroundRefresh_DoesNotUndoRawViewportScrolling(t *testing.T) {
	m := diffModel(t, "multi_hunk", 5)

	// Scroll the viewport directly, the way pgdn or a mouse wheel does.
	m.viewport.SetYOffset(6)
	scrolled := m.viewport.YOffset
	if scrolled == 0 {
		t.Fatal("precondition: viewport should have scrolled")
	}

	// The 2s poll reloads the same diff.
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: false})
	m = updated.(Model)

	if m.viewport.YOffset != scrolled {
		t.Errorf("poll scrolled back to %d, want the user's position %d", m.viewport.YOffset, scrolled)
	}
}

// When the diff really did change, the cursor must stay visible.
func TestBackgroundRefresh_ChangedDiffKeepsCursorVisible(t *testing.T) {
	m := diffModel(t, "multi_hunk", 5)
	m = press(t, m, "G")

	// A genuinely different diff arrives for the same file.
	parsed := ParseDiff(testutil.Fixture(t, "tabs_indent").Diff)
	r := NewDiffRenderer(parsed, "src.ts", m.styles, m.theme, 80)
	updated, _ := m.handleDiffLoaded(diffLoadedMsg{renderer: r, index: 0, resetScroll: false})
	m = updated.(Model)

	row, ok := m.renderer.RowFor(m.diffCursor)
	if !ok {
		t.Fatal("cursor has no row")
	}
	if row < m.viewport.YOffset || row >= m.viewport.YOffset+m.viewport.Height {
		t.Errorf("cursor row %d outside viewport [%d,%d)", row, m.viewport.YOffset, m.viewport.YOffset+m.viewport.Height)
	}
}

// #3: a hunk comment spans the whole hunk, so it must not swallow every line
// comment inside it.
func TestComment_HunkCommentDoesNotBlockLineComments(t *testing.T) {
	m := reviewOnAddedLine(t)

	// Comment the whole hunk first.
	updated, _ := m.updateDiffMode(key("C"))
	m = updated.(Model)
	m = typeText(t, m, "hunk note")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	// c on a line inside that hunk must start a new line comment, not reopen
	// the hunk comment.
	updated, _ = m.updateDiffMode(key("c"))
	m = updated.(Model)
	if !m.commenting {
		t.Fatal("c did not open an editor")
	}
	if m.commentInput.Value() == "hunk note" {
		t.Error("c reopened the hunk comment instead of starting a line comment")
	}
	if m.editingID != "" {
		t.Errorf("expected a new comment, got an edit of %s", m.editingID)
	}

	m = typeText(t, m, "line note")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if got := m.session.CountFor("src.ts"); got != 2 {
		t.Errorf("got %d comments, want 2 (hunk + line)", got)
	}
}

func TestComment_LineCommentIsPreferredForEditAndDelete(t *testing.T) {
	m := reviewOnAddedLine(t)

	updated, _ := m.updateDiffMode(key("C"))
	m = updated.(Model)
	m = typeText(t, m, "hunk note")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	updated, _ = m.updateDiffMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "line note")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	// c on that line now edits the line comment, not the hunk comment.
	updated, _ = m.updateDiffMode(key("c"))
	m = updated.(Model)
	if m.commentInput.Value() != "line note" {
		t.Errorf("c should edit the line comment, editor holds %q", m.commentInput.Value())
	}
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	// x removes the line comment and leaves the hunk comment alone.
	updated, _ = m.updateDiffMode(key("x"))
	m = updated.(Model)

	left := m.session.CommentsFor("src.ts")
	if len(left) != 1 {
		t.Fatalf("got %d comments after delete, want 1", len(left))
	}
	if left[0].Body != "hunk note" {
		t.Errorf("x deleted the wrong comment, %q remains", left[0].Body)
	}
}

// With only a hunk comment present, c still edits it — there is nothing else
// the cursor could mean.
func TestComment_HunkCommentIsStillEditableWhenItIsTheOnlyOne(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateDiffMode(key("C"))
	m = updated.(Model)
	m = typeText(t, m, "hunk note")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	// Move to a line with no comment of its own but inside the hunk, then
	// delete: the hunk comment is the only candidate.
	updated, _ = m.updateDiffMode(key("x"))
	m = updated.(Model)
	if got := m.session.CountFor("src.ts"); got != 0 {
		t.Errorf("hunk comment should still be reachable, %d remain", got)
	}
}
