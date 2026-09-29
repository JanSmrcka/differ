package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// Every size the frame agrees to draw must actually draw. minHeight came down
// to 8 in this branch, which made heights 8 and 9 reachable for the first time
// — and the comment editor's footer is six rows tall, so the content area went
// negative and View() panicked on make([]string, -1).
func TestFrame_DrawsAtEverySizeItAccepts(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nchanged\n")

	for _, w := range []int{40, 60, 64, 71, 72, 80, 120} {
		for _, h := range []int{8, 9, 10, 12, 24} {
			m := liveModel(t, tr)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m = updated.(Model)
			m.mode = modeReview
			m.commenting = true

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%dx%d with the comment editor open panicked: %v", w, h, r)
					}
				}()
				got := len(strings.Split(m.View(), "\n"))
				if got > h {
					t.Errorf("%dx%d drew %d rows, which is taller than the terminal", w, h, got)
				}
			}()
		}
	}
}

// Below twoPanelWidth only one panel is drawn, and which one depends on the
// mode. The widths depended on the mode too, so a resize taken in the file list
// built the diff viewport at width 0 — and pressing enter switched to a panel
// that had no room to render anything. The diff is what differ is for; it must
// never be blank because of how the user got there.
func TestResponsive_TheDiffIsVisibleInACollapsedLayout(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nchanged\n")

	for _, w := range []int{40, 50, 64, 71, 72, 100} {
		m := liveModel(t, tr)
		// The resize arrives while the user is still in the file list, which is
		// where every session starts.
		m = settle(t, m, tea.WindowSizeMsg{Width: w, Height: 24})
		if m.mode != modeFileList {
			t.Fatalf("%d: expected to start in the file list", w)
		}
		m = settle(t, m, key("enter")) // enter opens the diff

		if m.viewport.Width <= 0 {
			t.Errorf("%d cols: the diff viewport is %d wide", w, m.viewport.Width)
		}
		if !strings.Contains(m.View(), "changed") {
			t.Errorf("%d cols: the diff does not show the changed line:\n%s", w, m.View())
		}
	}
}

// settle delivers a message and runs whatever commands come back, the way the
// bubbletea runtime would. A resize returns loadDiffCmd; dropping it leaves the
// viewport empty for reasons that have nothing to do with the code under test.
func settle(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	for i := 0; msg != nil && i < 16; i++ {
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		if cmd == nil {
			break
		}
		msg = cmd()
	}
	return m
}

// With no files there is no diff, so the empty state should have the whole
// terminal to explain itself. It was being padded and cut to the file-list
// panel instead — 24 columns at 80 — while the panel beside it stayed blank:
// "Your working tree is c…" next to nothing at all.
func TestResponsive_TheEmptyStateGetsTheWholeWidth(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")

	for _, w := range []int{40, 72, 80, 100, 120} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: w, Height: 24})
		if len(m.files) != 0 {
			t.Fatalf("%d: expected a clean tree, got %d files", w, len(m.files))
		}
		view := m.View()
		if strings.Contains(view, "…") || strings.Contains(view, "›") {
			t.Errorf("%d cols: the empty state is truncated:\n%s", w, view)
		}
	}
}

// Widening the terminal must never turn split view off. It did: at 60-71
// columns the layout is collapsed so the diff has the full width and split
// engaged, and at 72 the file list reappears and cut the diff to 45 — so
// dragging a pane one column wider dropped the user out of split view. The
// README promises the opposite.
func TestResponsive_SplitViewNeverTurnsOffAsTheTerminalGrows(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nchanged\n")

	wasSplit := false
	for w := 40; w <= 160; w++ {
		m := liveModel(t, tr)
		m.splitDiff = true
		m = settle(t, m, tea.WindowSizeMsg{Width: w, Height: 24})
		m = settle(t, m, key("enter"))

		split := m.renderer != nil && m.renderer.split
		if wasSplit && !split {
			t.Fatalf("split view was on at %d columns and off at %d", w-1, w)
		}
		wasSplit = split
	}
}

// The branch filter should use the panel it sits in. It was pinned to
// minListWidth-8 — a floor, not the actual width — so it stayed 16 columns
// wide inside a 44-column list at 200 columns, and inside a full-width panel
// when the layout collapsed.
func TestResponsive_TheBranchFilterUsesThePanelItSitsIn(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	for _, w := range []int{60, 80, 200} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: w, Height: 24})
		if got, want := m.branchFilter.Width, m.listWidth()-8; got != want {
			t.Errorf("%d cols: filter is %d wide, want %d (list is %d)", w, got, want, m.listWidth())
		}
	}
}

// Concrete widths at concrete terminal sizes.
//
// The suite could not tell the new listWidth() from the fixed 35 it replaced:
// 35 sits inside [24,44], a constant is trivially monotone, and at 80 columns
// the diff still came out wider than it. Every assertion passed for the old
// code, so the change this PR exists to make was defended by nothing.
func TestResponsive_TheFileListIsAShareOfTheTerminal(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	for _, tc := range []struct{ width, list, diff int }{
		{60, 60, 60},   // collapsed: one panel, full width
		{80, 24, 53},   // 80/4 = 20, lifted to the minimum
		{120, 30, 87},  // 120/4 = 30, the share itself
		{200, 44, 153}, // 200/4 = 50, capped
	} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: tc.width, Height: 24})
		if got := m.listWidth(); got != tc.list {
			t.Errorf("%d cols: list is %d, want %d", tc.width, got, tc.list)
		}
		if got := m.diffWidth(); got != tc.diff {
			t.Errorf("%d cols: diff is %d, want %d", tc.width, got, tc.diff)
		}
	}
}

// The wall has to be where the constants say it is. The existing sweep asserted
// a wall at 30x6 and none at 60x10 — both of which held under the old 60x10
// minimum too, so lowering it to 40x8 was untested.
func TestResponsive_TheWallIsExactlyWhereTheMinimumSays(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	const wall = "Terminal too small"
	for _, tc := range []struct {
		w, h    int
		blocked bool
	}{
		{minWidth, minHeight, false},    // 40x8 draws — the point of the change
		{minWidth - 1, minHeight, true}, // 39x8 does not
		{minWidth, minHeight - 1, true}, // 40x7 does not
		{50, 8, false}, {40, 9, false},  // inside the new range, outside the old
	} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: tc.w, Height: tc.h})
		if got := strings.Contains(m.View(), wall); got != tc.blocked {
			t.Errorf("%dx%d: blocked=%v, want %v", tc.w, tc.h, got, tc.blocked)
		}
	}
}
