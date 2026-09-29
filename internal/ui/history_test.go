package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
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
