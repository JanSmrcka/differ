package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/testutil"
)

func modalModel(t *testing.T, w, h int) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\nfour\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\nfour\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: w, Height: h})
	return settle(t, m, key("r"))
}

// A modal is a box over the view, not a replacement for it: the frame stays
// exactly the terminal, nothing overflows, and the view shows around the box.
func TestModal_IsABoxOverTheViewAtEverySize(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 60, 80, 120, 200} {
		for _, h := range []int{10, 14, 24, 40} {
			m := modalModel(t, w, h)
			updated, _ := m.updateReviewMode(key("c"))
			m = updated.(Model)
			view := m.View()

			rows := strings.Split(view, "\n")
			if len(rows) != h {
				t.Errorf("%dx%d: frame is %d rows", w, h, len(rows))
			}
			for i, row := range rows {
				if got := lipgloss.Width(row); got > w {
					t.Errorf("%dx%d: row %d is %d wide", w, h, i, got)
					break
				}
			}
		}
	}
}

// Covering a row must not mark it as truncated. truncateEnd appends the
// truncation glyph, which put a "…" against the left edge of every modal row.
func TestModal_CoveringARowDoesNotMarkItTruncated(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 96, 22)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	for i, row := range strings.Split(m.View(), "\n") {
		if !strings.Contains(row, "│") && !strings.Contains(row, "╭") {
			continue // not a row the modal covers
		}
		if strings.Contains(row, truncationMarker) {
			t.Errorf("row %d is marked truncated where the modal covers it:\n%s", i, row)
		}
	}
}

// The view is still there around the box — that is the point of a modal over
// a footer bar.
func TestModal_TheViewShowsAroundIt(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 24)
	before := m.View()
	if !strings.Contains(before, "CHANGED FILES") {
		t.Fatal("the file list is not on screen to begin with")
	}

	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	view := m.View()

	if !strings.Contains(view, "CHANGED FILES") {
		t.Errorf("the modal blanked the view behind it:\n%s", view)
	}
	if !strings.Contains(view, "comment · line") {
		t.Errorf("the modal is not drawn:\n%s", view)
	}
}

// Only one question at a time.
func TestModal_OnlyOneIsEverDrawn(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 24)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m.showAgents = true // both flags set, which should not be reachable

	view := m.View()
	corners := strings.Count(view, "╭")
	if corners != 1 {
		t.Errorf("%d modals drawn at once:\n%s", corners, view)
	}
}

// You have to be able to see what you are typing.
//
// The box's body was whatever was left after the border and four rows of
// chrome, so below 13 rows the textarea got none at all: you typed and
// nothing appeared, and at 8 and 9 the closing line was cut too, so nothing
// on screen said how to get out. The footer editor it replaced guaranteed one
// row. minHeight is 8, so every one of those sizes is one differ agrees to
// draw.
func TestModal_TheTextAreaIsVisibleAtEveryHeight(t *testing.T) {
	t.Parallel()
	for _, h := range []int{8, 9, 10, 11, 12, 13, 14, 20, 40} {
		m := modalModel(t, 100, h)
		updated, _ := m.updateReviewMode(key("c"))
		m = updated.(Model)
		m.commentInput.SetValue("HELLOWORLD")

		view := m.View()
		if !strings.Contains(view, "HELLOWORLD") {
			t.Errorf("h=%d: what the user typed is not on screen:\n%s", h, view)
		}
		if !strings.Contains(view, "esc") {
			t.Errorf("h=%d: nothing says how to get out:\n%s", h, view)
		}
	}
}

// The modal must not cover the line the comment is about. A centred box and a
// cursor the diff keeps near the centre collide by construction.
func TestModal_DoesNotCoverTheLineBeingCommentedOn(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	body := strings.Repeat("keep\n", 60)
	tr.CommitFile("src.ts", body+"two\n", "first")
	tr.Modify("src.ts", body+"THE LINE I AM COMMENTING ON\n")

	for _, h := range []int{14, 20, 24, 30, 40} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: h})
		m = settle(t, m, key("r"))
		// Scroll to the changed line, which is near the end.
		for i := 0; i < 70; i++ {
			updated, _ := m.updateReviewMode(key("j"))
			m = updated.(Model)
		}
		if !strings.Contains(m.View(), "THE LINE I AM COMMENTING ON") {
			t.Fatalf("h=%d: the line is not on screen before the editor opens", h)
		}

		updated, _ := m.updateReviewMode(key("c"))
		m = updated.(Model)
		if !strings.Contains(m.View(), "THE LINE I AM COMMENTING ON") {
			t.Errorf("h=%d: the modal covers the line being commented on:\n%s", h, m.View())
		}
	}
}

// What is right of the box stays. Blanking it wiped most of the diff on every
// covered row at a wide terminal, for no reason.
func TestModal_KeepsWhatIsRightOfTheBox(t *testing.T) {
	t.Parallel()
	const width = 200
	bottom := strings.Repeat("R", width)
	box := "╭────╮"
	row := overlayRow(bottom, strings.Repeat(" ", 90)+box, width)

	if lipgloss.Width(row) != width {
		t.Fatalf("row is %d wide, want %d", lipgloss.Width(row), width)
	}
	if !strings.Contains(row, box) {
		t.Fatalf("the box is not in the row: %q", row)
	}
	after := row[strings.Index(row, box)+len(box):]
	if !strings.Contains(after, "R") {
		t.Errorf("everything right of the box was blanked:\n%q", row)
	}
}

// While a modal is open the bar says what that modal answers — not keys that
// would type a character into it.
func TestModal_TheBarAdvertisesOnlyTheModalsKeys(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 30)

	updated, _ := m.updateReviewMode(key("c"))
	editing := updated.(Model)
	bar := editing.View()
	if !strings.Contains(bar, "ctrl+s") {
		t.Errorf("the bar does not say how to save:\n%s", bar)
	}
	for _, typed := range []string{"c comment", "C hunk comment", "r exit review"} {
		if strings.Contains(bar, typed) {
			t.Errorf("the bar offers %q, which only types a character:\n%s", typed, bar)
		}
	}

	picking, _ := m.openAgentPicker()
	picking = withAgents(t, picking)
	if got := picking.View(); strings.Contains(got, "tab stage") {
		t.Errorf("the bar offers keys the picker swallows:\n%s", got)
	}
}

// boxGeometry measures the drawn box: the column its left edge starts at, its
// outer width, and the rows it occupies.
func boxGeometry(t *testing.T, view string) (lead, width, first, last int) {
	t.Helper()
	lead, width, first, last = -1, 0, -1, -1
	// Keyed on the corners, not on "│": the two-panel layout draws a vertical
	// rule between the file list and the diff, and looking for any vertical
	// glyph found that instead of the box.
	for i, row := range strings.Split(view, "\n") {
		if at := strings.Index(row, "╭"); at >= 0 {
			first, lead = i, lipgloss.Width(row[:at])
			if right := strings.Index(row, "╮"); right > at {
				width = lipgloss.Width(row[:right]) - lead + 1
			}
		}
		if strings.Contains(row, "╰") {
			last = i
		}
	}
	return lead, width, first, last
}

// The box's size and placement were asserted nowhere: making modalWidth
// return the whole terminal left the suite green, because the only geometry
// check was that no row exceeded the screen — which a full-width box does not.
//
// modalWidth also has to mean what it says. lipgloss draws the border outside
// the width it is given, so rendering at modalWidth() made every box two
// columns wider than the constant named.
func TestModal_IsTheWidthItSaysItIs(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 60, 80, 120, 200, 220} {
		m := modalModel(t, w, 40)
		updated, _ := m.updateReviewMode(key("c"))
		m = updated.(Model)

		lead, width, _, _ := boxGeometry(t, m.View())
		want := m.modalWidth()

		if width != want {
			t.Errorf("w=%d: box is %d columns wide, modalWidth() says %d", w, width, want)
		}
		if width < modalMinWidth && width < w {
			t.Errorf("w=%d: box is %d wide, below modalMinWidth %d", w, width, modalMinWidth)
		}
		if width > modalMaxWidth {
			t.Errorf("w=%d: box is %d wide, above modalMaxWidth %d", w, width, modalMaxWidth)
		}
		if lead <= 0 {
			t.Errorf("w=%d: box starts at column %d, so there is nothing to its left", w, lead)
		}
		if lead+width >= w {
			t.Errorf("w=%d: box ends at column %d, so there is nothing to its right", w, lead+width)
		}
	}
}

// The box lives inside the content area, between the two rules. Letting it
// have four rows more than the area — height-2 raised to height+4 — left the
// suite green, because the only geometry assertion was that no row is wider
// than the screen.
func TestModal_StaysInsideTheContentArea(t *testing.T) {
	t.Parallel()
	for h := commentModalMinHeight; h <= 60; h++ {
		m := modalModel(t, 120, h)
		updated, _ := m.updateReviewMode(key("c"))
		m = updated.(Model)
		view := m.View()

		rows := strings.Split(view, "\n")
		var rules []int
		for i, row := range rows {
			if trimmed := strings.TrimSpace(row); trimmed != "" && strings.Trim(trimmed, "─") == "" {
				rules = append(rules, i)
			}
		}
		if len(rules) != 2 {
			t.Fatalf("h=%d: the frame has %d rules, want 2:\n%s", h, len(rules), view)
		}
		_, _, first, last := boxGeometry(t, view)
		if first < 0 {
			t.Fatalf("h=%d: no box drawn", h)
		}
		if first <= rules[0] || last >= rules[1] {
			t.Errorf("h=%d: the box spans rows %d-%d, outside the content area %d-%d:\n%s",
				h, first, last, rules[0]+1, rules[1]-1, view)
		}
	}
}

// Which modal wins when two ask at once. The count of corners says one is
// drawn; it does not say which, and reversing the precedence left it green.
// The picker only ever opens over the editor because a send failed, and the
// answer it wants is the more urgent one.
func TestModal_ThePickerWinsOverTheEditor(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 30)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m.showAgents, m.agentsScanned = true, true
	m.agents = []feedback.Agent{{Pane: "%1", Session: "work", Window: "2", Tool: "claude"}}

	view := m.View()
	if !strings.Contains(view, "work:2") {
		t.Errorf("the picker is not the modal drawn:\n%s", view)
	}
	_, _, first, last := boxGeometry(t, view)
	box := strings.Join(strings.Split(view, "\n")[first:last+1], "\n")
	if strings.Contains(box, "ctrl+s") {
		t.Errorf("the comment editor is the box drawn:\n%s", box)
	}
}

// What is left of the box stays there. Dropping the left padding shifted the
// box whenever the row underneath was shorter than the box's own indent, and
// nothing noticed.
func TestModal_KeepsWhatIsLeftOfTheBox(t *testing.T) {
	t.Parallel()
	got := overlayRow(strings.Repeat("L", 8), strings.Repeat(" ", 20)+"BOX", 40)

	if !strings.HasPrefix(got, strings.Repeat("L", 8)) {
		t.Errorf("the row underneath lost its left edge: %q", got)
	}
	if at := strings.Index(got, "BOX"); at != 20 {
		t.Errorf("the box moved to column %d, want 20: %q", at, got)
	}
}

// A hunk comment covers a range of lines and the title says so. Collapsing it
// to a single line left the suite green.
func TestModal_AHunkCommentsTitleSaysTheRange(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 30)
	updated, _ := m.updateReviewMode(key("C"))
	m = updated.(Model)

	title := m.commentTitle()
	if !strings.Contains(title, "-") {
		t.Errorf("a hunk comment's title is %q, which names no range", title)
	}
}

// The width the model wraps at and the width the box draws at have to be the
// same number. View sized a value copy, so the model kept the diff panel's
// width — 167 against 82 drawn at 220 columns — and the editor's own idea of
// where a line ends was not the one on screen.
func TestModal_TheEditorWrapsWhereItIsDrawn(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 80, 120, 220} {
		m := modalModel(t, w, 30)
		updated, _ := m.updateReviewMode(key("c"))
		m = updated.(Model)

		// The draw path calls SetWidth on a value copy. If the model already
		// holds that width, doing it again changes nothing; if it does not,
		// the two disagree about where a line ends.
		sized := m.commentInput
		sized.SetWidth(m.commentEditorWidth())
		if got, want := sized.Width(), m.commentInput.Width(); got != want {
			t.Errorf("w=%d: the box draws the editor at %d, the model wraps at %d", w, got, want)
		}
		// And what is drawn actually fits inside the box.
		_, boxWidth, _, _ := boxGeometry(t, m.View())
		if m.commentEditorWidth() > boxWidth {
			t.Errorf("w=%d: the editor is %d wide inside a %d-wide box",
				w, m.commentEditorWidth(), boxWidth)
		}
	}
}

// renderModal is given a body that its callers have already fitted, so the
// bound it puts on its own height is never reached through the UI — raising
// height-2 to height+4 left every screen identical. It is still the thing
// standing between a long body and a box taller than the area it is drawn
// in, so it is checked directly.
func TestModal_ABodyTooLongForTheRoomIsStillABox(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 30)
	body := make([]string, 100)
	for i := range body {
		body[i] = "row " + strconv.Itoa(i)
	}

	for _, height := range []int{6, 8, 12, 19, 28} {
		out := m.renderModal(" long", body, "esc cancels", height, -1)

		rows := strings.Split(out, "\n")
		if len(rows) != height {
			t.Errorf("height=%d: renderModal returned %d rows", height, len(rows))
		}
		_, _, first, last := boxGeometry(t, out)
		if first < 0 || last < 0 {
			t.Errorf("height=%d: no box drawn:\n%s", height, out)
			continue
		}
		if last >= height {
			t.Errorf("height=%d: the box runs to row %d", height, last)
		}
	}
}
