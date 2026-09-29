package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The session history. Mid-review it is easy to lose track of what has already
// gone to the agent — and a send that failed is the thing most worth being
// able to look up.

func historyModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.mode = modeReview
	m.session = review.NewSession()
	return m
}

func TestHistory_ASuccessfulSendIsRecorded(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	c := m.session.Add(review.Comment{File: "a.ts", StartLine: 3, EndLine: 3, Body: "rename this"})

	updated, _ := m.handleFeedbackSent(feedbackSentMsg{ids: []string{c.ID}, target: "clipboard"})
	m = updated.(Model)

	h := m.session.History()
	if len(h) != 1 {
		t.Fatalf("history has %d entries, want 1", len(h))
	}
	if h[0].Target != "clipboard" || !h[0].OK() {
		t.Errorf("recorded %+v, want a successful clipboard delivery", h[0])
	}
	if got := h[0].Files; len(got) != 1 || got[0] != "a.ts" {
		t.Errorf("Files = %v, want [a.ts]", got)
	}
	if h[0].At.IsZero() {
		t.Error("the delivery has no time on it")
	}
}

// A failure is recorded and the comment stays pending, so it can be retried.
func TestHistory_AFailedSendIsRecordedAndNothingIsMarkedSent(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	c := m.session.Add(review.Comment{File: "a.ts", StartLine: 3, EndLine: 3, Body: "rename this"})

	updated, _ := m.handleFeedbackSent(feedbackSentMsg{ids: []string{c.ID}, target: "tmux", err: errPaneGone{}})
	m = updated.(Model)

	h := m.session.History()
	if len(h) != 1 {
		t.Fatalf("history has %d entries, want 1", len(h))
	}
	if h[0].OK() {
		t.Error("a failed delivery was recorded as a success")
	}
	if !strings.Contains(h[0].Err, "pane") {
		t.Errorf("Err = %q, want the target's own words", h[0].Err)
	}
	if got := m.session.CommentsFor("a.ts")[0].State; got != review.StatePending {
		t.Errorf("comment state = %v, want still pending", got)
	}
}

type errPaneGone struct{}

func (errPaneGone) Error() string { return "tmux pane %9 is gone" }

// H opens the history, and the overlay has to answer the question that made
// the user press it: what went out, where to, and did it work.
func TestHistory_HOpensAnOverlayNamingTargetAndOutcome(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	c := m.session.Add(review.Comment{File: "a.ts", StartLine: 3, EndLine: 3, Body: "rename this"})
	updated, _ := m.handleFeedbackSent(feedbackSentMsg{ids: []string{c.ID}, target: "tmux", err: errPaneGone{}})
	m = updated.(Model)

	updated, _ = m.routeKey(key("H"))
	m = updated.(Model)
	if !m.showHistory {
		t.Fatal("H did not open the history")
	}

	view := stripANSI(m.renderHistoryOverlay(120, 20))
	for _, want := range []string{"tmux", "a.ts", "failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("the history does not mention %q:\n%s", want, view)
		}
	}

	// Built by hand: key() only makes single runes and quietly returns a down
	// arrow for anything longer, which would test the wrong key entirely.
	updated, _ = m.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).showHistory {
		t.Error("esc did not close the history")
	}
}

// Nothing sent yet is a normal state, not a blank panel.
func TestHistory_AnEmptyHistorySaysSo(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	if got := stripANSI(m.renderHistoryOverlay(80, 10)); !strings.Contains(got, "nothing sent") {
		t.Errorf("an empty history renders %q, want it to say nothing has been sent", got)
	}
}

// The reason review mode exists: the agent rewrites a file while the user is
// reading a different one. That file is no longer reviewed, and the file list
// has to say so.
func TestProgress_ARefreshMarksTheFilesThatChanged(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.files = []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified, AddedLines: 1}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified, AddedLines: 1}},
	}
	m.fileKeys = map[string]string{"a.ts": "k1", "b.ts": "k1"}
	m.session.MarkViewed("a.ts")
	m.session.MarkViewed("b.ts")

	// b.ts was rewritten; a.ts is the one on screen and was not.
	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: m.files,
		keys:  map[string]string{"a.ts": "k1", "b.ts": "k2"},
	})
	m = updated.(Model)

	if !m.session.ChangedSinceViewed("b.ts") {
		t.Error("b.ts changed under the reviewer and was not marked")
	}
	if m.session.ChangedSinceViewed("a.ts") {
		t.Error("a.ts did not change but was marked")
	}
}

// The file being read cannot be stale to its reader: whatever arrives is what
// they are looking at.
func TestProgress_TheFileOnScreenIsNeverMarkedChanged(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.files = []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}}
	m.fileKeys = map[string]string{"a.ts": "k1"}
	m.session.MarkViewed("a.ts")

	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{files: m.files, keys: map[string]string{"a.ts": "k2"}})
	if updated.(Model).session.ChangedSinceViewed("a.ts") {
		t.Error("the file on screen was marked changed")
	}
}

// The progress readout is the one place the whole review is summarised, so a
// file that went stale under the reviewer has to appear in it.
func TestProgress_TheSummaryReportsChangedFiles(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.files = []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
	}
	m.session.MarkViewed("b.ts")
	m.session.NoteChange("b.ts")

	got := m.reviewSummary()
	if !strings.Contains(got, "1 changed") {
		t.Errorf("summary = %q, want it to report 1 changed file", got)
	}
	if !strings.Contains(got, "0/2") {
		t.Errorf("summary = %q, want the changed file to stop counting as reviewed", got)
	}
}

// An overlay wider than the terminal is the failure the overlays were drawn
// over the panels to avoid: the row soft-wraps, the body gains a line, and the
// bottom rule and command bar are pushed off screen.
func TestOverlays_RowsNeverExceedTheWidth(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.session.RecordDelivery(review.Delivery{
		At:       time.Date(2026, 9, 29, 16, 39, 6, 0, time.UTC),
		Target:   "tmux",
		Comments: []string{"c1", "c2", "c3"},
		Files: []string{
			"internal/api/client/transport.ts",
			"internal/auth/login/session.ts",
			"internal/store/reducers/user.ts",
		},
	})

	// 60 is minWidth, the narrowest terminal differ still draws.
	for _, width := range []int{60, 80, 120} {
		for _, overlay := range []struct {
			name string
			body string
		}{
			{"history", m.renderHistoryOverlay(width, 12)},
			{"help", m.renderHelpOverlay(width, 24)},
		} {
			for _, row := range strings.Split(overlay.body, "\n") {
				if got := lipgloss.Width(row); got > width {
					t.Errorf("%s overlay at width %d has a %d-column row: %q",
						overlay.name, width, got, stripANSI(row))
				}
			}
		}
	}
}

// An overlay must not be left orphaned by something arriving in the
// background. Opening the branch picker is asynchronous, so the mode can
// change while the history is on screen — and the branch picker is a text
// input, which used to mean every key went into the filter and the overlay
// could not be closed at all.
func TestOverlays_SurviveAModeChangeArrivingFromTheBackground(t *testing.T) {
	t.Parallel()
	m := historyModel(t)

	updated, _ := m.routeKey(key("H"))
	m = updated.(Model)
	if !m.showHistory {
		t.Fatal("H did not open the history")
	}

	// The branch list lands while the overlay is open.
	after, _ := m.handleBranchesLoaded(branchesLoadedMsg{branches: []string{"master", "topic"}, current: "master"})
	m = after.(Model)
	if m.mode != modeBranchPicker {
		t.Fatalf("mode = %v, want the branch picker", m.mode)
	}
	if m.showHistory {
		t.Error("the history is still drawn over a view it has nothing to do with")
	}

	// And an overlay open over a text input still closes.
	m.showHistory = true
	closed, _ := m.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(Model).showHistory {
		t.Error("esc did not close an overlay over the branch picker")
	}
}

// Through View(), not the renderer: the overlay has to actually replace the
// panels, and must not change the layout's height while it is open — the diff
// viewport has to come back exactly where it was.
func TestOverlays_ViewDrawsThemOverThePanelsWithoutResizing(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	m.mode = modeReview
	m.session = review.NewSession()
	m.session.RecordDelivery(review.Delivery{
		At: time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC), Target: "clipboard",
		Comments: []string{"c1"}, Files: []string{"src.ts"},
	})

	plain := lipgloss.Height(m.View())

	m.showHistory = true
	view := m.View()
	if !strings.Contains(stripANSI(view), "sent this session") {
		t.Errorf("View does not draw the history:\n%s", stripANSI(view))
	}
	if got := lipgloss.Height(view); got != plain {
		t.Errorf("the history changed the layout height: %d rows, was %d", got, plain)
	}

	m.showHelp, m.showHistory = true, false
	if got := lipgloss.Height(m.View()); got != plain {
		t.Errorf("the help overlay changed the layout height: %d rows, was %d", got, plain)
	}
}

// Clipping a row that is already styled is not a rune operation: an escape
// sequence measures zero columns, so cutting runes off the end can drop the
// reset — and the colour then bleeds into the rest of the screen — or cut an
// escape in half.
//
// lipgloss emits no escapes under `go test` (there is no TTY), so the row is
// built the way a real terminal would receive it.
func TestOverlays_ClippingAStyledRowLeavesTheTerminalClean(t *testing.T) {
	t.Parallel()
	row := "\x1b[38;2;147;153;178m16:39:06\x1b[0m  2 comments → tmux  " +
		"\x1b[38;2;147;153;178minternal/api/client/transport.ts\x1b[0m"

	for _, width := range []int{20, 40, 57, 58, 59} {
		got := padTo(clipOverlayRow(row, width), width)

		if w := lipgloss.Width(got); w != width {
			t.Errorf("width %d: clipped row is %d columns", width, w)
		}
		if i := strings.LastIndex(got, "\x1b"); i >= 0 && !strings.Contains(got[i:], "m") {
			t.Errorf("width %d: truncated escape sequence: %q", width, got[i:])
		}
		// The *last* sequence has to be the reset. An earlier one does not
		// help: whatever colour was set after it is still active when the row
		// ends, and it carries on down the screen.
		if last := lastEscape(got); last != "" && last != "\x1b[0m" {
			t.Errorf("width %d: row ends with %q still in effect: %q", width, last, got)
		}
	}
}

// lastEscape is the final SGR sequence in s, or "" when there is none.
func lastEscape(s string) string {
	i := strings.LastIndex(s, "\x1b[")
	if i < 0 {
		return ""
	}
	if j := strings.IndexByte(s[i:], 'm'); j >= 0 {
		return s[i : i+j+1]
	}
	return s[i:]
}

// The files are what the overlay exists to answer, and joined onto the summary
// they were the first thing clipped away on a narrow terminal.
func TestOverlays_TheHistoryKeepsItsFilesOnANarrowTerminal(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.session.RecordDelivery(review.Delivery{
		At: time.Date(2026, 9, 29, 13, 4, 5, 0, time.UTC), Target: "clipboard",
		Comments: []string{"c1", "c2", "c3"},
		Files: []string{
			"internal/ui/diffrender.go",
			"internal/review/session.go",
			"internal/git/repo.go",
		},
	})

	got := stripANSI(m.renderHistoryOverlay(60, 12))
	for _, want := range []string{"diffrender.go", "session.go", "repo.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("the history lost %q at width 60:\n%s", want, got)
		}
	}
}

// Dropping rows off the end took the closing line with them: at the minimum
// terminal the help overlay showed three of twenty-one keys and no way out.
func TestOverlays_TheClosingLineSurvivesASmallPanel(t *testing.T) {
	t.Parallel()
	m := historyModel(t)

	for _, height := range []int{1, 2, 3, 4, 5, 8, 10, 24} {
		for _, o := range []struct {
			name, body string
		}{
			{"help", m.renderHelpOverlay(80, height)},
			{"history", m.renderHistoryOverlay(80, height)},
		} {
			rows := strings.Split(o.body, "\n")
			if len(rows) != max(height, 0) {
				t.Errorf("%s at height %d rendered %d rows", o.name, height, len(rows))
				continue
			}
			// Four rows is the least that can carry a title and a way out.
			if height >= 4 && !strings.Contains(stripANSI(o.body), "esc to close") {
				t.Errorf("%s at height %d has no way out:\n%s", o.name, height, stripANSI(o.body))
			}
		}
	}
}

// Silently showing three of twenty-one keys is worse than saying so.
func TestOverlays_WhatWasDroppedIsCounted(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.mode = modeReview

	full := strings.Count(stripANSI(m.renderHelpOverlay(80, 40)), "\n")
	got := stripANSI(m.renderHelpOverlay(80, 8))
	if !strings.Contains(got, "more") {
		t.Errorf("the help overlay dropped rows without saying so (%d rows in full):\n%s", full, got)
	}
}

// A history with entries must never claim nothing was sent, whatever the room.
func TestOverlays_ANonEmptyHistoryNeverClaimsNothingWasSent(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.session.RecordDelivery(review.Delivery{
		At: time.Date(2026, 9, 29, 13, 4, 5, 0, time.UTC), Target: "clipboard",
		Comments: []string{"c1"}, Files: []string{"a.ts"},
	})

	for height := 1; height <= 12; height++ {
		if got := stripANSI(m.renderHistoryOverlay(80, height)); strings.Contains(got, "nothing sent") {
			t.Errorf("height %d claims nothing was sent:\n%s", height, got)
		}
	}
}

// H is a review-mode key, not a global. With the file list's help open it was
// reaching the overlay's own switch and opening a history the file list does
// not offer and the README does not document.
func TestOverlays_HDoesNotOpenTheHistoryWhereItIsNotABinding(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.mode = modeFileList
	m.showHelp = true

	updated, _ := m.routeKey(key("H"))
	got := updated.(Model)
	if got.showHistory {
		t.Error("H opened the history from the file list's help overlay")
	}
	if !got.showHelp {
		t.Error("H closed the help overlay instead of being ignored")
	}

	// In review mode, where it is a binding, it still closes the history.
	m = historyModel(t)
	m.showHistory = true
	if updated, _ = m.routeKey(key("H")); updated.(Model).showHistory {
		t.Error("H did not close the history")
	}
}

// A delivery can cover more files than there are columns. Dividing the room
// between them gave each a zero-column budget, and truncatePath returns ""
// for that — so the row came out as a line of bare commas.
func TestOverlays_ManyFilesInOneDeliveryStillReadAsFiles(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	files := make([]string, 60)
	for i := range files {
		files[i] = fmt.Sprintf("internal/pkg%02d/file.go", i)
	}
	m.session.RecordDelivery(review.Delivery{
		At: time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC), Target: "clipboard",
		Comments: []string{"c1"}, Files: files,
	})

	for _, width := range []int{60, 80, 120} {
		got := stripANSI(m.renderHistoryOverlay(width, 12))
		if !strings.Contains(got, "file.go") {
			t.Errorf("width %d: no file name survived:\n%s", width, got)
		}
		if strings.Contains(got, ", , ") {
			t.Errorf("width %d: the names were truncated away to nothing:\n%s", width, got)
		}
		// The rest has to be accounted for, not silently dropped.
		if !strings.Contains(got, "more") {
			t.Errorf("width %d: %d files listed with no sign of the rest:\n%s", width, len(files), got)
		}
	}
}
