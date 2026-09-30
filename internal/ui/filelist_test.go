package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The changed-file list is the primary navigator, and it had a plain bug: it
// stopped rendering at the panel height with no scrolling, so in a changeset
// larger than the panel the rest of the files were unreachable. Agent
// changesets are routinely that large.

// manyFiles builds a changeset larger than any panel.
func manyFiles(n int) []fileItem {
	files := make([]fileItem, 0, n)
	for i := range n {
		files = append(files, fileItem{change: git.FileChange{
			Path:       "src/file" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".ts",
			Status:     git.StatusModified,
			AddedLines: i,
		}})
	}
	return files
}

func TestFileList_ACursorPastThePanelScrollsIntoView(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 20 // a panel far smaller than the changeset

	// Walk to the last file the way a user would.
	for range 39 {
		updated, _ := m.updateFileListMode(key("j"))
		m = updated.(Model)
	}
	if m.cursor != 39 {
		t.Fatalf("cursor = %d, want 39", m.cursor)
	}

	shown := m.renderFileList()
	last := m.files[39].change.Path
	if !strings.Contains(stripANSI(shown), "file"+string(rune('a'+39%26))+string(rune('0'+39/26))) {
		t.Errorf("the last file (%s) is not on screen:\n%s", last, stripANSI(shown))
	}
}

// The list showed filepath.Base only, so src/a/index.ts and src/b/index.ts
// were the same two words — in a changeset full of index.ts and page.tsx, the
// navigator told you nothing.
func TestFileList_FilesWithTheSameBasenameAreDistinguishable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		paths []string
		want  map[string]string
	}{
		{
			name:  "unique basenames stay short",
			paths: []string{"src/api/client.ts", "src/auth/login.ts"},
			want:  map[string]string{"src/api/client.ts": "client.ts", "src/auth/login.ts": "login.ts"},
		},
		{
			name:  "a clash adds one directory",
			paths: []string{"src/a/index.ts", "src/b/index.ts"},
			want:  map[string]string{"src/a/index.ts": "a/index.ts", "src/b/index.ts": "b/index.ts"},
		},
		{
			name:  "only as much as it takes",
			paths: []string{"x/deep/a/index.ts", "y/deep/b/index.ts", "z/other.ts"},
			want: map[string]string{
				"x/deep/a/index.ts": "a/index.ts",
				"y/deep/b/index.ts": "b/index.ts",
				"z/other.ts":        "other.ts",
			},
		},
		{
			name:  "a clash that goes all the way up",
			paths: []string{"a/x/index.ts", "b/x/index.ts"},
			want:  map[string]string{"a/x/index.ts": "a/x/index.ts", "b/x/index.ts": "b/x/index.ts"},
		},
		{
			name:  "one file needs nothing",
			paths: []string{"only/one.ts"},
			want:  map[string]string{"only/one.ts": "one.ts"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := shortNames(c.paths)
			for path, want := range c.want {
				if got[path] != want {
					t.Errorf("%s → %q, want %q", path, got[path], want)
				}
			}
		})
	}
}

// Through the renderer, not just the helper.
func TestFileList_TheRenderedListDisambiguatesPaths(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "src/a/index.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "src/b/index.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "src/api/client.ts", Status: git.StatusModified}},
	})
	got := stripANSI(m.renderFileList())

	for _, want := range []string{"a/index.ts", "b/index.ts"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list does not distinguish %q:\n%s", want, got)
		}
	}
	// The file with no clash keeps its short name.
	if !strings.Contains(got, "client.ts") {
		t.Errorf("an unambiguous file lost its short name:\n%s", got)
	}
	if strings.Contains(got, "src/api/client.ts") {
		t.Errorf("an unambiguous file shows its whole path:\n%s", got)
	}
}

// Review state is the column the issue asked for: how far you have got with
// each file, without opening it.
func TestFileList_ShowsReviewState(t *testing.T) {
	t.Parallel()
	// The names are deliberately meaningless: naming a file "sent.ts" makes
	// `Contains(got, "sent")` pass whether or not the badge is rendered, which
	// is how two assertions here used to be unfalsifiable.
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "one.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "two.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "three.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "four.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "five.ts", Status: git.StatusModified}},
	})
	m.mode = modeReview
	m.session = review.NewSession()
	m.session.MarkViewed("one.ts")
	m.session.Add(review.Comment{File: "two.ts", StartLine: 1, EndLine: 1, Body: "x"})
	delivered := m.session.Add(review.Comment{File: "three.ts", StartLine: 1, EndLine: 1, Body: "y"})
	m.session.MarkSent([]string{delivered.ID})
	m.session.MarkViewed("four.ts")
	m.session.NoteChange("four.ts")

	rowFor := func(path string) string {
		for _, line := range strings.Split(stripANSI(m.renderFileList()), "\n") {
			if strings.Contains(line, path) {
				return line
			}
		}
		t.Fatalf("no row for %s", path)
		return ""
	}

	for _, c := range []struct{ path, want string }{
		{"one.ts", "read"},
		{"two.ts", "1 comment"},
		{"three.ts", "sent"},
		{"four.ts", "changed"},
	} {
		if row := rowFor(c.path); !strings.Contains(row, c.want) {
			t.Errorf("%s should be badged %q: %q", c.path, c.want, row)
		}
	}
	// A file nobody has looked at needs no badge — that is the normal state,
	// so its row carries the line counts like any other.
	if row := rowFor("five.ts"); !strings.Contains(row, "+0 -0") {
		t.Errorf("an unreviewed file lost its line counts: %q", row)
	}
}

// Outside review mode there is no review state to show, and the column would
// be dead space in the narrowest panel differ has.
func TestFileList_NoReviewColumnOutsideReviewMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.session = review.NewSession()
	m.session.MarkViewed("a.ts")
	m.session.NoteChange("a.ts")
	m.mode = modeFileList

	if got := stripANSI(m.renderFileList()); strings.Contains(got, "changed") {
		t.Errorf("review state leaked into the plain file list:\n%s", got)
	}
}

// Staged, then unstaged, then untracked — so the list reads in the order the
// user is going to act on it, and the staged block is not scattered through
// the rest.
func TestFileList_IsOrderedByStagedThenUnstagedThenUntracked(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("staged.ts", "one\n", "init")
	tr.CommitFile("unstaged.ts", "one\n", "second")
	tr.Modify("staged.ts", "one\ntwo\n")
	tr.Stage("staged.ts")
	tr.Modify("unstaged.ts", "one\ntwo\n")
	tr.Untracked("new.ts", "fresh\n")

	m := liveModel(t, tr)
	var order []string
	for _, f := range m.files {
		order = append(order, f.change.Path)
	}
	want := []string{"staged.ts", "unstaged.ts", "new.ts"}
	if len(order) != len(want) {
		t.Fatalf("files = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("files = %v, want %v", order, want)
			break
		}
	}
}

// A changeset that shrinks — the agent commits half its work — must not leave
// the window scrolled past the end of the list, showing nothing.
func TestFileList_TheWindowFollowsAShrinkingChangeset(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 20
	m.cursor = 39
	m = m.clampFileScroll()
	if m.fileOffset == 0 {
		t.Fatal("the window never scrolled")
	}

	// Most of the changeset is committed away.
	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: manyFiles(3),
		keys:  map[string]string{},
	})
	m = updated.(Model)

	if got := stripANSI(m.renderFileList()); strings.TrimSpace(got) == "" {
		t.Errorf("the list is empty after the changeset shrank (offset %d, %d files)", m.fileOffset, len(m.files))
	}
}

// Every row is exactly the panel width, with the right-hand column against the
// right edge. Ragged stats are what made the old list hard to scan, and a row
// wider than the panel would push the diff out of alignment.
func TestFileList_RowsAreExactlyThePanelWideWithTheRightColumnAligned(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified, AddedLines: 1, DeletedLines: 1}},
		{change: git.FileChange{Path: "src/deeply/nested/and/very/long/name.ts", Status: git.StatusModified, AddedLines: 1234, DeletedLines: 5678}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusAdded, AddedLines: 9}},
	})

	rows := strings.Split(m.renderFileList(), "\n")
	if len(rows) != 3 {
		t.Fatalf("rendered %d rows, want 3", len(rows))
	}
	var ends []int
	for i, row := range rows {
		if got := lipgloss.Width(row); got != m.listWidth() {
			t.Errorf("row %d is %d columns, want %d: %q", i, got, m.listWidth(), stripANSI(row))
		}
		// Columns, not bytes: a byte count would call any non-ASCII name
		// ragged.
		ends = append(ends, lipgloss.Width(strings.TrimRight(stripANSI(row), " ")))
	}
	for i := 1; i < len(ends); i++ {
		if ends[i] != ends[0] {
			t.Errorf("the right column is ragged: rows end at %v\n%s", ends, stripANSI(strings.Join(rows, "\n")))
			break
		}
	}
}

// A resize changes how many rows the list gets, and the offset is the only
// piece of list state that depends on it. Growing the terminal past the whole
// changeset used to leave the window where it was, so the files above it went
// back to being unreachable — the very bug the offset was added to fix.
func TestFileList_AResizeBringsTheWindowBack(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 30
	m.ready = true

	updated, _ := m.updateFileListMode(key("G"))
	m = updated.(Model)
	if m.fileOffset == 0 {
		t.Fatal("G did not scroll the window")
	}

	// Grown past the whole changeset: everything fits, so nothing is hidden.
	grown, _ := m.handleResize(tea.WindowSizeMsg{Width: 120, Height: 60})
	g := grown.(Model)
	if g.fileOffset != 0 {
		t.Errorf("the whole changeset fits but the window starts at %d", g.fileOffset)
	}
	if first := stripANSI(strings.Split(g.renderFileList(), "\n")[0]); !strings.Contains(first, "filea0") {
		t.Errorf("the first file is not on screen after growing: %q", first)
	}

	// Shrunk: the cursor has to still be visible.
	shrunk, _ := m.handleResize(tea.WindowSizeMsg{Width: 120, Height: 12})
	s := shrunk.(Model)
	if s.cursor < s.fileOffset || s.cursor >= s.fileOffset+s.listHeight() {
		t.Errorf("cursor %d is outside the window [%d, %d)", s.cursor, s.fileOffset, s.fileOffset+s.listHeight())
	}
}

// The window must not be left past the end of a list that shrank under a
// cursor which is still inside the old window — the case the earlier test
// claimed to cover and did not, because it moved the cursor first.
func TestFileList_TheWindowIsPulledBackWhenTheListShrinksUnderIt(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 30
	m.cursor = 30
	m = m.clampFileScroll()
	if m.fileOffset == 0 {
		t.Fatal("the window never scrolled")
	}

	// Half the changeset is committed away; the cursor lands inside what is
	// left, so only the end-of-list clamp can save the window.
	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{files: manyFiles(20), keys: map[string]string{}})
	m = updated.(Model)

	rows := strings.Split(m.renderFileList(), "\n")
	if got := len(rows); got != min(20, m.listHeight()) {
		t.Errorf("rendered %d rows from %d files in a %d-row panel (offset %d)",
			got, len(m.files), m.listHeight(), m.fileOffset)
	}
}

// Non-ASCII names are the common case for some of us, and the truncation used
// to slice bytes: the row came out as mojibake.
func TestFileList_TruncatingANonASCIINameKeepsItReadable(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   string
		maxW int
	}{
		{"žluťoučký.ts", 4},
		{"日本語のファイル.ts", 8},
		{"src/components/žluťoučký-kůň.tsx", 12},
		{"src.ts", 1},
		{"src.ts", 0},
	} {
		got := truncatePath(c.in, c.maxW)
		if !utf8.ValidString(got) {
			t.Errorf("truncatePath(%q, %d) = %q, which is not valid UTF-8", c.in, c.maxW, got)
		}
		if w := lipgloss.Width(got); w > c.maxW && c.maxW > 0 {
			t.Errorf("truncatePath(%q, %d) = %q, %d columns wide", c.in, c.maxW, got, w)
		}
	}
}

// A frame must not cost rows × files. The disambiguation looks at every path
// in the changeset, and computing it inside the row renderer made a thousand
// files cost 9 ms and 39 MB of garbage per keypress.
//
// The assertion is a ratio rather than a wall-clock budget: the suite runs in
// parallel, so absolute timings here are whatever the rest of it leaves of the
// CPU, while both halves of a ratio are inflated equally. The same number of
// rows is drawn either way, so the only thing that grows with the changeset is
// one pass over the paths.
//
// The ratio is taken from the *fastest* batch of several rather than from one
// batch each. Both halves are inflated equally only on average: a scheduler
// preemption inside the 100-file batch alone raised the ratio above four with
// the code unchanged, which failed this test twice in one afternoon for
// reasons that had nothing to do with the file list. The minimum of several
// batches is the one least interfered with, and interference can only ever
// make a batch slower.
func TestFileList_RenderingDoesNotCostRowsTimesFiles(t *testing.T) {
	t.Parallel()
	frame := func(n int) time.Duration {
		m := newTestModel(t, manyFiles(n))
		m.height = 60
		_ = m.renderFileList() // warm the styles

		best := time.Duration(math.MaxInt64)
		for range 5 {
			start := time.Now()
			const runs = 20
			for range runs {
				_ = m.renderFileList()
			}
			best = min(best, time.Since(start)/runs)
		}
		return best
	}

	small, large := frame(100), frame(1000)
	// Quadratic in the changeset would be about 10×; linear is a little over
	// 1×. Four is comfortably between the two and well clear of noise.
	if large > 4*small {
		t.Errorf("a 1000-file frame took %v against %v for 100 files — %0.1f×",
			large, small, float64(large)/float64(small))
	}
}

// The branch picker shares the panel and had the same two holes: no
// end-of-list clamp, so growing the terminal left its window past the tail,
// and nothing clamped when the list first loaded — with the current branch
// past the panel height, the picker opened with no visible selection.
func TestBranchList_TheWindowAndTheCursorStayTogether(t *testing.T) {
	t.Parallel()
	branches := make([]string, 40)
	for i := range branches {
		branches[i] = fmt.Sprintf("branch-%02d", i)
	}

	// Opening on a branch past the panel height must scroll to it.
	m := newTestModel(t, nil)
	m.height = 30
	m.ready = true
	loaded, _ := m.handleBranchesLoaded(branchesLoadedMsg{branches: branches, current: "branch-37"})
	m = loaded.(Model)
	if m.branchCursor != 37 {
		t.Fatalf("branchCursor = %d, want 37", m.branchCursor)
	}
	if !strings.Contains(stripANSI(m.renderBranchList(m.listHeight())), "branch-37") {
		t.Errorf("the picker opened without the current branch on screen (offset %d)", m.branchOffset)
	}

	// And growing past the whole list must bring the window back.
	grown, _ := m.handleResize(tea.WindowSizeMsg{Width: 120, Height: 60})
	g := grown.(Model)
	if !strings.Contains(stripANSI(g.renderBranchList(g.listHeight())), "branch-00") {
		t.Errorf("the whole list fits but it starts at %d", g.branchOffset)
	}
}

// The right-hand column decides its text and its colour together, and the row
// arithmetic is done against the text — so what is rendered has to be exactly
// that text. Doubling the stale marker in one branch and not the other made
// rows two columns wider than the panel with the whole suite green.
func TestFileList_TheRightColumnRendersExactlyWhatItMeasured(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified, AddedLines: 12, DeletedLines: 3}}})
	styles := m.styles

	for _, badge := range []string{"", "read", "1 comment", "sent", "changed"} {
		for _, stale := range []bool{false, true} {
			c := rightColumn{stale: stale, added: 12, deleted: 3}
			if badge != "" {
				c.badge, c.text = true, badge
			} else {
				c.text = "+12 -3"
			}
			if stale {
				c.text = staleMarker + " " + c.text
			}

			if got := stripANSI(c.render(styles)); got != c.text {
				t.Errorf("badge=%q stale=%v: rendered %q but measured %q", badge, stale, got, c.text)
			}
		}
	}
}
