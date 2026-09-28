package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// A hunk whose first content line is a removal: the anchor must be chosen on
// the same side the comment is addressed on, or it can never be found again.
func TestHunkComment_SurvivesReanchorWhenHunkStartsWithARemoval(t *testing.T) {
	raw := "@@ -1,3 +1,3 @@\n-alpha\n+beta\n ctx1\n ctx2\n"
	parsed := ParseDiff(raw)

	m := diffModel(t, "multi_hunk", 20)
	m.renderer = NewDiffRenderer(parsed, "a.ts", m.styles, m.theme, 80)
	m.files[0].change.Path = "a.ts"
	m.session = review.NewSession()
	m = m.setCursor(parsed.FirstCommentableLine())

	c, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("buildHunkComment failed")
	}
	stored := m.session.Add(c)

	// Re-anchor against the very same diff: nothing changed, so nothing should
	// go stale.
	m.session.Reanchor("a.ts", diffLocations(parsed))

	got, _ := m.session.Get(stored.ID)
	if got.State != review.StateStale {
		return // healthy, as it should be
	}
	t.Errorf("hunk comment went stale against an identical diff: %s\n  side=%v anchor=%q",
		got.StaleReason, c.Side, c.Anchor)
}

// #1: the confirmation must be required per send, not once per session.
func TestStaleConfirm_IsRequiredEveryTime(t *testing.T) {
	m, tr := reviewOnRepo(t)
	fake := feedback.NewFake()
	m.target = fake

	stale := func() Model {
		content := tr.Read("src.ts")
		tr.ExternalEdit("src.ts", strings.Replace(content, "getUser(id", "getUser(id, opts", 1))
		return reload(t, m)
	}

	m = stale()
	// First stale comment: warn, then send.
	u, _ := m.updateReviewMode(key("S"))
	m = u.(Model)
	u, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)
	if len(fake.Sent()) != 1 {
		t.Fatalf("expected the confirmed send, got %d payloads", len(fake.Sent()))
	}

	// A second stale comment must warn again rather than going straight out.
	m = commentAt(t, m, LineContext, "  return user", "another note")
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "return user", "return user!", 1))
	m = reload(t, m)

	u, cmd = m.updateReviewMode(key("S"))
	m = u.(Model)
	if cmd != nil {
		t.Error("the stale guard fired only once — a later stale comment sent without confirmation")
	}
	if !strings.Contains(m.statusMsg, "stale") {
		t.Errorf("status = %q, want a fresh stale warning", m.statusMsg)
	}
}

// Any other key must clear a pending stale confirmation.
func TestStaleConfirm_ClearedByOtherKeys(t *testing.T) {
	m, tr := reviewOnRepo(t)
	m.target = feedback.NewFake()
	content := tr.Read("src.ts")
	tr.ExternalEdit("src.ts", strings.Replace(content, "getUser(id", "getUser(id, opts", 1))
	m = reload(t, m)

	u, _ := m.updateReviewMode(key("S"))
	m = u.(Model)
	if !m.staleConfirm {
		t.Fatal("precondition: the guard should be armed")
	}

	u, _ = m.updateReviewMode(key("j"))
	m = u.(Model)
	if m.staleConfirm {
		t.Error("moving the cursor should disarm the stale confirmation")
	}
}

// #3: a delivered comment must not be re-sent after its file leaves the diff.
func TestSend_DoesNotResendDeliveredCommentsThatWentStale(t *testing.T) {
	m, tr := reviewOnRepo(t)
	fake := feedback.NewFake()
	m.target = fake

	u, cmd := m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)
	if len(fake.Sent()) != 1 {
		t.Fatalf("precondition: expected one send, got %d", len(fake.Sent()))
	}

	// The agent applies the fix and commits, so the file leaves the diff.
	tr.Stage("src.ts")
	tr.Commit("apply the review")
	m = reload(t, m)

	u, cmd = m.updateReviewMode(key("S"))
	if cmd != nil {
		t.Error("there is nothing left to send, so S should not dispatch")
	}
	m = u.(Model)
	u, cmd = m.updateReviewMode(key("S"))
	m = runCmd(t, u.(Model), cmd)

	if len(fake.Sent()) != 1 {
		t.Errorf("an already-delivered comment was sent again (%d payloads)", len(fake.Sent()))
	}
}

// #5: a failed refresh must not stale the whole review.
func TestRefreshFailure_DoesNotStaleEverything(t *testing.T) {
	m, _ := reviewOnRepo(t)

	// A refresh that failed carries no files; it must not be read as "every
	// file left the diff".
	u, _ := m.Update(filesRefreshedMsg{files: nil, err: errRefreshFailed})
	m = u.(Model)

	if got := m.session.Comments()[0]; got.State == review.StateStale {
		t.Errorf("a failed refresh staled the review: %s", got.StaleReason)
	}
}

// #4: comments on files other than the one on screen must be re-anchored too.
func TestReanchor_CoversFilesNotCurrentlyOpen(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.AgentChangeset()
	m := liveModel(t, tr)
	u, _ := m.updateFileListMode(key("r"))
	m = u.(Model)

	// Comment on login.ts, then move to a different file.
	for i, f := range m.files {
		if strings.HasSuffix(f.change.Path, "login.ts") {
			m.cursor = i
		}
	}
	if cmd := m.loadDiffCmd(true); cmd != nil {
		u, _ = m.Update(cmd())
		m = u.(Model)
	}
	m = commentAt(t, m, LineAdded, "  const user = await getUser(id)", "needs await")
	before := m.session.Comments()[0]

	m.cursor = 0
	if cmd := m.loadDiffCmd(true); cmd != nil {
		u, _ = m.Update(cmd())
		m = u.(Model)
	}

	// Now the agent edits login.ts while another file is on screen.
	content := tr.Read("src/auth/login.ts")
	tr.ExternalEdit("src/auth/login.ts", "// inserted\n// lines\n"+content)
	m = reload(t, m)
	if cmd := m.reanchorAllCmd(); cmd != nil {
		u, _ = m.Update(cmd())
		m = u.(Model)
	}

	got := m.session.Comments()[0]
	if got.StartLine == before.StartLine && got.State == review.StatePending {
		t.Errorf("comment on a background file was never re-anchored (still line %d)", got.StartLine)
	}
}

var _ = tea.Quit

// errRefreshFailed stands in for a transient git failure, e.g. index.lock held
// while the agent stages files.
var errRefreshFailed = errors.New("git status: index.lock exists")
