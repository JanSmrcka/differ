package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// sendModel is a review session with two comments and a fake target.
func sendModel(t *testing.T) (Model, *feedback.Fake) {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("r"))
	m = updated.(Model)

	m = commentAt(t, m, LineAdded, "  const user = await getUser(id)", "first note")
	m = commentAt(t, m, LineAdded, "  persist(data)", "second note")

	fake := feedback.NewFake()
	m.target = fake
	return m, fake
}

// runCmd executes a returned command and feeds the message back in.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func TestSend_OneCommentUnderTheCursor(t *testing.T) {
	m, fake := sendModel(t)
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	updated, cmd := m.updateReviewMode(key("s"))
	runCmd(t, updated.(Model), cmd)

	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d payloads, want 1", len(sent))
	}
	if !strings.Contains(sent[0], "first note") {
		t.Errorf("wrong comment sent:\n%s", sent[0])
	}
	if strings.Contains(sent[0], "second note") {
		t.Errorf("sending one comment sent both:\n%s", sent[0])
	}
}

func TestSend_OneCommentMarksOnlyItSent(t *testing.T) {
	m, _ := sendModel(t)
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	updated, cmd := m.updateReviewMode(key("s"))
	m = runCmd(t, updated.(Model), cmd)

	if got := m.session.SentCount(); got != 1 {
		t.Errorf("SentCount = %d, want 1", got)
	}
	if got := m.session.PendingCount(); got != 1 {
		t.Errorf("PendingCount = %d, want 1", got)
	}
}

func TestSend_AllPendingComments(t *testing.T) {
	m, fake := sendModel(t)

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("send-all should deliver one combined payload, got %d", len(sent))
	}
	if !strings.Contains(sent[0], "first note") || !strings.Contains(sent[0], "second note") {
		t.Errorf("send-all missed a comment:\n%s", sent[0])
	}
	if m.session.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", m.session.PendingCount())
	}
}

// The load-bearing guarantee of #41: a failed send loses nothing.
func TestSend_FailureKeepsCommentsPending(t *testing.T) {
	m, fake := sendModel(t)
	fake.Err = errors.New("clipboard unavailable")

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if got := m.session.PendingCount(); got != 2 {
		t.Errorf("PendingCount = %d, want 2 — a failed send must not mark anything sent", got)
	}
	if got := m.session.SentCount(); got != 0 {
		t.Errorf("SentCount = %d, want 0", got)
	}
	if !strings.Contains(m.statusMsg, "clipboard unavailable") {
		t.Errorf("the failure should be reported to the user, got %q", m.statusMsg)
	}
}

func TestSend_SuccessConfirmsToTheUser(t *testing.T) {
	m, _ := sendModel(t)

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if m.statusMsg == "" {
		t.Fatal("a successful send should confirm to the user")
	}
	if !strings.Contains(m.statusMsg, "sent") {
		t.Errorf("status = %q, want it to mention sending", m.statusMsg)
	}
}

func TestSend_NothingPendingIsReported(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("r"))
	m = updated.(Model)
	m.target = feedback.NewFake()

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if !strings.Contains(m.statusMsg, "no ") {
		t.Errorf("status = %q, want it to say there is nothing to send", m.statusMsg)
	}
}

func TestSend_SentCommentsAreNotResent(t *testing.T) {
	m, fake := sendModel(t)

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)
	updated, cmd = m.updateReviewMode(key("S"))
	runCmd(t, updated.(Model), cmd)

	if n := len(fake.Sent()); n != 1 {
		t.Errorf("already-sent comments were sent again (%d payloads)", n)
	}
}

func TestSend_SentCommentsRenderAsSent(t *testing.T) {
	m, _ := sendModel(t)

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	view := m.View()
	if !strings.Contains(view, "sent") {
		t.Errorf("sent comments should be labelled in the diff:\n%s", view)
	}
}

func TestSend_StatusBarCountsSent(t *testing.T) {
	m, _ := sendModel(t)
	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if bar := m.statusSegment(); !strings.Contains(bar, "2 sent") {
		t.Errorf("status bar should report sent comments: %q", bar)
	}
}

func TestSend_WithNoTargetConfiguredReportsTheProblem(t *testing.T) {
	m, _ := sendModel(t)
	m.target = nil
	m.targetErr = errors.New("no clipboard command found")

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if !strings.Contains(m.statusMsg, "no clipboard command found") {
		t.Errorf("status = %q, want the configuration error", m.statusMsg)
	}
	if m.session.PendingCount() != 2 {
		t.Error("comments must stay pending when there is no target")
	}
}

func TestSend_PayloadIsTheFormattedFeedback(t *testing.T) {
	m, fake := sendModel(t)
	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	want := review.FormatFeedback(m.session.Comments())
	if got := fake.Sent()[0]; got != want {
		t.Errorf("payload is not the formatted feedback:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSend_RealClipboardTargetIsResolvedFromConfig(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)

	// liveModel uses config.Default(), which leaves feedback_target empty.
	if m.targetErr != nil {
		t.Skipf("no clipboard command on this machine: %v", m.targetErr)
	}
	if m.target == nil {
		t.Fatal("default config should resolve a target")
	}
	if m.target.Name() != "clipboard" {
		t.Errorf("default target = %q, want clipboard", m.target.Name())
	}
}

// The complete loop: a comment written in review mode arrives in another tmux
// pane as text a coding agent can act on.
func TestSend_EndToEndIntoATmuxPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	outfile := filepath.Join(dir, "agent-received.txt")
	sess := "differ-e2e-test"
	_ = exec.Command("tmux", "kill-session", "-t", sess).Run()
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sess, "-n", "w", "cat > "+outfile).CombinedOutput(); err != nil {
		t.Skipf("cannot start tmux session: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })

	paneOut, err := exec.Command("tmux", "list-panes", "-t", sess+":w", "-F", "#{pane_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	pane := strings.TrimSpace(string(paneOut))

	m, _ := sendModel(t)
	target, err := feedback.Resolve(feedback.Config{Target: "tmux", TmuxTarget: pane})
	if err != nil {
		t.Skipf("tmux target unavailable: %v", err)
	}
	m.target = target

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if m.session.PendingCount() != 0 {
		t.Errorf("comments still pending after a successful send: %q", m.statusMsg)
	}

	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(outfile); err == nil {
			got = string(data)
			if strings.Contains(got, "second note") {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, want := range []string{"Review feedback", "File: src.ts", "first note", "second note"} {
		if !strings.Contains(got, want) {
			t.Errorf("the agent pane never received %q; got:\n%s", want, got)
		}
	}
	t.Logf("payload delivered to pane %s:\n%s", pane, got)
}

// #4 end to end: with the stdout target the payload must survive the TUI and
// reach the terminal after it exits, not be painted onto the alt screen.
func TestSend_StdoutTargetSurvivesTheTUI(t *testing.T) {
	m, _ := sendModel(t)
	target, err := feedback.Resolve(feedback.Config{Target: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	m.target = target

	updated, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, updated.(Model), cmd)

	if m.session.PendingCount() != 0 {
		t.Fatalf("comments still pending: %q", m.statusMsg)
	}

	var out strings.Builder
	if err := m.FlushFeedback(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Review feedback", "first note", "second note"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("flushed output missing %q:\n%s", want, out.String())
		}
	}
}
