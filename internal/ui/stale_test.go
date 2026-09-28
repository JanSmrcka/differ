package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// reviewOnRepo puts a comment on the awaited line of a real repo and returns
// the model plus the testutil repo so the file can be edited underneath.
func reviewOnRepo(t *testing.T) (Model, *testutil.Repo) {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	u, _ := m.updateFileListMode(key("r"))
	m = u.(Model)
	m = commentAt(t, m, LineAdded, "  const user = await getUser(id)", "keep this awaited")
	return m, tr
}

// reload re-runs the diff load the way the poll does.
func reload(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(m.buildRefreshedFiles())
	m = u.(Model)
	if cmd := m.loadDiffCmd(false); cmd != nil {
		u, _ = m.Update(cmd())
		m = u.(Model)
	}
	return m
}

// An edit elsewhere in the file must leave the comment anchored.
func TestStale_UnrelatedEditKeepsTheComment(t *testing.T) {
	m, tr := reviewOnRepo(t)
	before := m.session.Comments()[0]

	// Change the second hunk, far from the comment.
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "return true", "return false", 1))
	m = reload(t, m)

	got := m.session.Comments()[0]
	if got.State != review.StatePending {
		t.Errorf("State = %v (%s), want pending", got.State, got.StaleReason)
	}
	if got.Anchor != before.Anchor {
		t.Errorf("anchor changed from %q to %q", before.Anchor, got.Anchor)
	}
}

// The agent applies the fix: the commented line is gone, so the comment is
// stale rather than pointing at whatever took its place.
func TestStale_AgentFixingTheLineMarksItStale(t *testing.T) {
	m, tr := reviewOnRepo(t)

	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content,
		"const user = await getUser(id)", "const user = await getUser(id, opts)", 1))
	m = reload(t, m)

	got := m.session.Comments()[0]
	if got.State != review.StateStale {
		t.Errorf("State = %v, want stale", got.State)
	}
	if got.StaleReason == "" {
		t.Error("stale comment should explain why")
	}
}

// Line numbers move but the line survives: the comment follows it.
func TestStale_InsertedLinesShiftTheComment(t *testing.T) {
	m, tr := reviewOnRepo(t)
	before := m.session.Comments()[0]

	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", "// added header\n// another\n"+content)
	m = reload(t, m)

	got := m.session.Comments()[0]
	if got.State != review.StatePending {
		t.Fatalf("State = %v (%s), want pending", got.State, got.StaleReason)
	}
	if got.StartLine <= before.StartLine {
		t.Errorf("StartLine = %d, want it shifted past %d", got.StartLine, before.StartLine)
	}
}

// The whole file goes away.
func TestStale_DeletedFileStalesItsComments(t *testing.T) {
	m, tr := reviewOnRepo(t)

	tr.Stage("src.ts")
	tr.Commit("accept the change")
	m = reload(t, m)

	got := m.session.Comments()[0]
	if got.State != review.StateStale {
		t.Errorf("State = %v, want stale once the file left the diff", got.State)
	}
	if !strings.Contains(got.StaleReason, "file") {
		t.Errorf("StaleReason = %q, want it to mention the file", got.StaleReason)
	}
}

func TestStale_RenderedDistinctlyInTheDiff(t *testing.T) {
	m, tr := reviewOnRepo(t)
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "await getUser(id)", "await getUser(id, o)", 1))
	m = reload(t, m)

	if !strings.Contains(m.View(), "stale") {
		t.Errorf("stale state is not visible in the diff:\n%s", m.View())
	}
}

func TestStale_CountedInTheStatusBar(t *testing.T) {
	m, tr := reviewOnRepo(t)
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "await getUser(id)", "await getUser(id, o)", 1))
	m = reload(t, m)

	if bar := m.renderStatusBar(); !strings.Contains(bar, "stale") {
		t.Errorf("status bar does not report stale comments: %q", bar)
	}
}

// Sending a stale comment must take an explicit second action.
func TestStale_SendingRequiresConfirmation(t *testing.T) {
	m, tr := reviewOnRepo(t)
	fake := feedback.NewFake()
	m.target = fake

	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "await getUser(id)", "await getUser(id, o)", 1))
	m = reload(t, m)

	u, cmd := m.updateReviewMode(key("S"))
	m = u.(Model)
	if cmd != nil {
		t.Error("sending stale comments should not go through on the first press")
	}
	if !strings.Contains(m.statusMsg, "stale") {
		t.Errorf("status = %q, want a warning about stale comments", m.statusMsg)
	}
	if len(fake.Sent()) != 0 {
		t.Error("nothing should have been sent yet")
	}

	u, cmd = m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)
	if len(fake.Sent()) != 1 {
		t.Errorf("a second S should send, got %d payloads", len(fake.Sent()))
	}
}

// A healthy comment sends without any confirmation.
func TestStale_HealthyCommentsSendImmediately(t *testing.T) {
	m, _ := reviewOnRepo(t)
	fake := feedback.NewFake()
	m.target = fake

	u, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)

	if len(fake.Sent()) != 1 {
		t.Errorf("got %d payloads, want 1", len(fake.Sent()))
	}
}

func TestStale_FileListMarksFilesWithStaleComments(t *testing.T) {
	m, tr := reviewOnRepo(t)
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "await getUser(id)", "await getUser(id, o)", 1))
	m = reload(t, m)

	list := m.renderFileList(m.contentHeight())
	if !strings.Contains(list, staleMarker) {
		t.Errorf("file list should mark a file with stale comments:\n%s", list)
	}
}

func TestStale_ReasonShownWhenCursorIsOnIt(t *testing.T) {
	m, tr := reviewOnRepo(t)
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "await getUser(id)", "await getUser(id, o)", 1))
	m = reload(t, m)

	// The rendered comment block should carry the reason, so the user can see
	// what changed without leaving the diff.
	if !strings.Contains(m.View(), "no longer") {
		t.Errorf("the stale reason is not shown:\n%s", m.View())
	}
}
