package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

func viewHeight(m Model) int { return len(strings.Split(m.View(), "\n")) }

func lipglossWidth(s string) int { return lipgloss.Width(s) }

// #1: the whole layout must fit the terminal in every mode. The comment
// editor is five rows, so the cards have to give up that space.
func TestView_FitsTerminalHeightInEveryMode(t *testing.T) {
	const rows = 30
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	base := liveModel(t, tr)
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 120, Height: rows})
	base = updated.(Model)

	cases := map[string]func(Model) Model{
		"file list": func(m Model) Model { m.mode = modeFileList; return m },
		"diff":      func(m Model) Model { m.mode = modeDiff; return m },
		"review": func(m Model) Model {
			u, _ := m.updateFileListMode(key("r"))
			return u.(Model)
		},
		"comment editor": func(m Model) Model {
			u, _ := m.updateFileListMode(key("r"))
			u2, _ := u.(Model).updateReviewMode(key("c"))
			return u2.(Model)
		},
		"commit": func(m Model) Model {
			m.mode = modeCommit
			return m
		},
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m := setup(base)
			if got := viewHeight(m); got > rows {
				t.Errorf("%s renders %d lines, terminal has %d — the top of the layout is pushed off screen", name, got, rows)
			}
		})
	}
}

// The viewport must shrink with the cards, or the diff is clipped instead.
func TestCommentEditor_ShrinksTheDiffViewport(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)
	updated, _ = m.Update(key("r"))
	m = updated.(Model)
	before := m.viewport.Height

	updated, _ = m.updateReviewMode(key("c"))
	m = updated.(Model)
	open := m.viewport.Height
	if open >= before {
		t.Errorf("viewport height %d did not shrink when the editor opened (was %d)", open, before)
	}

	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.viewport.Height != before {
		t.Errorf("viewport height %d not restored after closing the editor (was %d)", m.viewport.Height, before)
	}
	// The viewport matches the panel's list rows, not the whole panel: two
	// rows go to the label and the blank line under it.
	if m.viewport.Height != m.listHeight() {
		t.Errorf("viewport height %d != listHeight %d", m.viewport.Height, m.listHeight())
	}
}

// #5: a single comment must not read "1 comments", and the title must not
// assume a session exists.
func TestDiffCardTitle_SingularCommentCount(t *testing.T) {
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.mode = modeReview
	m.session = review.NewSession()
	m.session.Add(review.Comment{File: "a.ts", Side: review.SideNew, StartLine: 1, EndLine: 1, Body: "x"})

	title := m.diffLabel()
	if strings.Contains(title, "1 comments") {
		t.Errorf("title says %q, want a singular form", title)
	}
	if !strings.Contains(title, "1 comment") {
		t.Errorf("title = %q, want it to mention one comment", title)
	}

	m.session.Add(review.Comment{File: "a.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "y"})
	if got := m.diffLabel(); !strings.Contains(got, "2 comments") {
		t.Errorf("title = %q, want plural for two", got)
	}
}

func TestDiffCardTitle_SurvivesAMissingSession(t *testing.T) {
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.mode = modeReview
	m.session = nil

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("diffCardTitle panicked without a session: %v", r)
		}
	}()
	if got := m.diffLabel(); !strings.Contains(got, "a.ts") {
		t.Errorf("title = %q", got)
	}
}

// #2: in split view a removed line and the added line replacing it share one
// display row, so marking the whole row makes the two indistinguishable.
func splitRenderer(t *testing.T) (*DiffRenderer, ParsedDiff) {
	t.Helper()
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	r.SetSplit(true)
	return r, parsed
}

func TestSplitCursor_SidesRenderDifferently(t *testing.T) {
	r, parsed := splitRenderer(t)
	removed := lineIndexOf(t, parsed, LineRemoved, "  const user = getUser(id)")
	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")

	if rr, _ := r.RowFor(removed); rr != mustRow(t, r, added) {
		t.Fatal("precondition: the two lines should share a row")
	}

	if r.Content(removed) == r.Content(added) {
		t.Error("cursor on the old side renders identically to the new side — the reviewer cannot tell which they are commenting on")
	}
}

func mustRow(t *testing.T, r *DiffRenderer, idx int) int {
	t.Helper()
	row, ok := r.RowFor(idx)
	if !ok {
		t.Fatalf("line %d has no row", idx)
	}
	return row
}

func TestSplitCursor_MarkerIsOnTheCursorSide(t *testing.T) {
	r, parsed := splitRenderer(t)
	removed := lineIndexOf(t, parsed, LineRemoved, "  const user = getUser(id)")
	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	row := mustRow(t, r, removed)

	left := strings.Split(r.Content(removed), "\n")[row]
	right := strings.Split(r.Content(added), "\n")[row]

	// The marker must sit before the separator for the old side and after it
	// for the new side.
	sepAt := strings.Index(left, "│")
	if sepAt < 0 {
		t.Fatalf("no split separator in row: %q", left)
	}
	if !strings.Contains(left[:sepAt], cursorMarker) {
		t.Errorf("cursor on the old side is not marked on the left: %q", left)
	}

	sepAt = strings.Index(right, "│")
	if strings.Contains(right[:sepAt], cursorMarker) {
		t.Errorf("cursor on the new side is marked on the left: %q", right)
	}
	if !strings.Contains(right[sepAt:], cursorMarker) {
		t.Errorf("cursor on the new side is not marked on the right: %q", right)
	}
}

// Moving between the two sides must visibly change the diff, or j appears to
// do nothing while silently flipping which side a comment lands on.
func TestSplitCursor_MovingBetweenSidesChangesTheView(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	m.mode = modeDiff
	m.splitDiff = true
	if cmd := m.loadDiffCmd(true); cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}

	m = cursorOn(t, m, LineRemoved, "  const user = getUser(id)")
	before := m.viewport.View()
	oldSide, _ := m.cursorAddress()

	m = press(t, m, "j")
	after := m.viewport.View()
	newSide, _ := m.cursorAddress()

	if oldSide.Type == newSide.Type {
		t.Fatalf("precondition: j should move onto the added line, still %v", newSide.Type)
	}
	if before == after {
		t.Error("moving from the old side to the new side rendered no change")
	}
}

// #3: the log browser went through its own renderer, so it did not honour
// cfg.TabWidth — it silently used lipgloss's hard-coded 4.
func TestLogBrowser_HonoursConfiguredTabWidth(t *testing.T) {
	styles, th := testStyles()
	const width = 100
	raw := "diff --git a/tabs.go b/tabs.go\n" + testutil.Fixture(t, "tabs_indent").Diff

	four := renderCommitDiff(raw, styles, th, width, 4)
	eight := renderCommitDiff(raw, styles, th, width, 8)

	if four == eight {
		t.Error("tab width has no effect on the log browser — it is not using the shared renderer")
	}
	for _, out := range []string{four, eight} {
		for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if strings.Contains(line, "\t") {
				t.Errorf("line %d has a raw tab: %q", i, line)
			}
			if got := lipglossWidth(line); got > width {
				t.Errorf("line %d is %d columns, panel is %d", i, got, width)
			}
		}
	}
}

// #4: the truncation marker is not file content and must not be counted.
func TestParseNewFile_TruncationMarkerIsNotPartOfTheHunk(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxDiffLines+50; i++ {
		b.WriteString("line\n")
	}
	parsed := ParseNewFile(b.String())

	if len(parsed.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(parsed.Hunks))
	}
	h := parsed.Hunks[0]

	last := parsed.Lines[len(parsed.Lines)-1]
	if last.Type != LineHunkHeader {
		t.Fatalf("precondition: expected a truncation marker, got %v", last.Type)
	}
	if h.LastLine != len(parsed.Lines)-2 {
		t.Errorf("LastLine = %d, want %d — the marker must be excluded", h.LastLine, len(parsed.Lines)-2)
	}
	if h.NewCount != maxDiffLines {
		t.Errorf("NewCount = %d, want %d", h.NewCount, maxDiffLines)
	}
}

func TestParseNewFile_TruncatedHunkCommentExcludesTheMarker(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxDiffLines+50; i++ {
		b.WriteString("line\n")
	}
	m := diffModel(t, "multi_hunk", 20)
	m.renderer = NewDiffRenderer(ParseNewFile(b.String()), "big.txt", m.styles, m.theme, 80)
	m.files[0].change.Path = "big.txt"
	m = m.setCursor(0)

	c, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.EndLine != maxDiffLines {
		t.Errorf("hunk comment covers lines %d-%d, want it to end at %d", c.StartLine, c.EndLine, maxDiffLines)
	}
	// The excerpt has its own length cap and may say "truncated" about that;
	// what must not appear is the diff's own marker text.
	if strings.Contains(c.Excerpt, fmt.Sprintf("%d+ lines", maxDiffLines)) {
		t.Errorf("the diff's truncation marker leaked into the excerpt as file content:\n%s", c.Excerpt)
	}
}

// #6: quitting with unsent comments must not silently discard them.
func TestQuit_WarnsAboutPendingComments(t *testing.T) {
	m := reviewOnAddedLine(t)
	u, _ := m.updateReviewMode(key("c"))
	m = u.(Model)
	m = typeText(t, m, "unsent note")
	u, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = u.(Model)

	// First q warns instead of quitting.
	u, cmd := m.updateReviewMode(key("q"))
	m = u.(Model)
	if cmd != nil {
		t.Error("q with pending comments should not quit immediately")
	}
	if !strings.Contains(m.statusMsg, "not sent") {
		t.Errorf("status = %q, want a warning that comments were not sent", m.statusMsg)
	}

	// Second q goes through.
	_, cmd = m.updateReviewMode(key("q"))
	if cmd == nil {
		t.Error("q again should quit")
	}
}

func TestQuit_ImmediateWhenNothingIsPending(t *testing.T) {
	m := reviewOnAddedLine(t)
	if _, cmd := m.updateReviewMode(key("q")); cmd == nil {
		t.Error("q with no comments should quit immediately")
	}
}

func TestQuit_FileListAlsoWarns(t *testing.T) {
	m := reviewOnAddedLine(t)
	u, _ := m.updateReviewMode(key("c"))
	m = u.(Model)
	m = typeText(t, m, "unsent note")
	u, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = u.(Model)

	// esc back to the file list, then q.
	u, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	u, cmd := m.updateFileListMode(key("q"))
	m = u.(Model)
	if cmd != nil {
		t.Error("q in the file list with pending comments should warn first")
	}
	if !strings.Contains(m.statusMsg, "not sent") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

func TestQuit_SentCommentsDoNotWarn(t *testing.T) {
	m, _ := sendModel(t)
	u, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)

	if _, cmd := m.updateReviewMode(key("q")); cmd == nil {
		t.Error("q after sending everything should quit immediately")
	}
}

// No hint may be dropped: the footer is allowed to wrap and footerHeight
// measures it, so the cards shrink instead of the layout overflowing.
func TestHelpBar_KeepsEveryHintAtEveryWidth(t *testing.T) {
	for _, mode := range []viewMode{modeFileList, modeDiff, modeReview, modeBranchPicker} {
		m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
		m.mode = mode
		for _, width := range []int{80, 100, 120} {
			m.width = width
			bar := m.renderHintBar()
			for _, p := range m.helpPairs() {
				if !strings.Contains(bar, p.desc) {
					t.Errorf("mode %d at width %d dropped hint %q %q", mode, width, p.key, p.desc)
				}
			}
		}
	}
}

func TestHelpBar_KeepsQuitAndReviewHints(t *testing.T) {
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
	m.mode = modeFileList
	m.width = 80

	bar := m.renderHintBar()
	for _, want := range []string{"q", "quit", "review", "commit", "stage"} {
		if !strings.Contains(bar, want) {
			t.Errorf("file list help lost %q: %q", want, bar)
		}
	}
}

// Narrow terminals are where a wrapped footer would overflow.
func TestView_FitsNarrowTerminal(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	base := liveModel(t, tr)

	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 40}} {
		u, _ := base.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m := u.(Model)

		u, _ = m.updateFileListMode(key("r"))
		review := u.(Model)
		u2, _ := review.updateReviewMode(key("c"))
		editing := u2.(Model)

		for name, got := range map[string]Model{"file list": m, "review": review, "editor": editing} {
			if h := viewHeight(got); h > size.h {
				t.Errorf("%s at %dx%d renders %d lines", name, size.w, size.h, h)
			}
			for i, line := range strings.Split(got.View(), "\n") {
				if w := lipglossWidth(line); w > size.w {
					t.Errorf("%s at %dx%d: line %d is %d columns", name, size.w, size.h, i, w)
					break
				}
			}
		}
	}
}
