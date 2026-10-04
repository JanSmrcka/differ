package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

func commentAt(t *testing.T, m Model, typ DiffLineType, content, body string) Model {
	t.Helper()
	m = cursorOn(t, m, typ, content)
	updated, _ := m.updateDiffMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, body)
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	return updated.(Model)
}

func TestFeedback_FromARealCommentHasCorrectContext(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	m = commentAt(t, m, LineAdded, "  const user = await getUser(id)", "keep this awaited")

	out := review.FormatFeedback(m.session.Comments())

	// A comment whose line resolves is the reference and the body — #90
	// removed the `File:`, `Line:` and excerpt sections, which restated what
	// the reference already carries.
	for _, want := range []string{
		"@src.ts :L2",
		"keep this awaited",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("feedback missing %q:\n%s", want, out)
		}
	}
	// And no code at all, from this hunk or any other: the agent reads it
	// from the reference. The second hunk leaking in was the original defect
	// this test was written for, and a payload with no code cannot leak one
	// — so the excerpt the comment still carries is checked where it is used,
	// in TestFormatComment_AnUnresolvableLineKeepsItsContext.
	if strings.Contains(out, "getUser(id)") || strings.Contains(out, "persist(data)") {
		t.Errorf("the payload carries code the reference already points at:\n%s", out)
	}
	// The comment itself still holds the excerpt, for the forms that need it.
	if c := m.session.Comments()[0]; !strings.Contains(c.Excerpt, "getUser(id)") {
		t.Errorf("the comment lost its excerpt: %q", c.Excerpt)
	}
}

// The headline #40 guarantee: the view the comment was written in must not
// change the feedback produced.
func TestFeedback_IdenticalWhetherWrittenInUnifiedOrSplit(t *testing.T) {
	build := func(split bool) string {
		tr := testutil.NewRepo(t)
		tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
		m := liveModel(t, tr)
		if split {
			m.splitDiff = true
			if cmd := m.loadDiffCmd(true); cmd != nil {
				updated, _ := m.Update(cmd())
				m = updated.(Model)
			}
		}
		updated, _ := m.updateFileListMode(key("enter"))
		m = updated.(Model)
		m = commentAt(t, m, LineAdded, "  persist(data)", "same note")
		return review.FormatFeedback(m.session.Comments())
	}

	unified, split := build(false), build(true)
	if unified != split {
		t.Errorf("feedback differs by view:\n--- unified ---\n%s\n--- split ---\n%s", unified, split)
	}
}

func TestFeedback_RemovedLineReportsTheOldSide(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	m = commentAt(t, m, LineRemoved, "  await persist(data)", "why was the await dropped?")

	out := review.FormatFeedback(m.session.Comments())
	if !strings.Contains(out, "Line: 11 (old)") {
		t.Errorf("removed line should be reported on the old side:\n%s", out)
	}
}

func TestFeedback_HunkCommentReportsTheRange(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")
	updated, _ = m.updateDiffMode(key("C"))
	m = updated.(Model)
	m = typeText(t, m, "restructure this")
	updated, _ = m.updateDiffMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	out := review.FormatFeedback(m.session.Comments())
	// The range is in the reference, which is where an agent's resolver
	// reads it: `Lines: 1-5 (new)` said the same thing a second time.
	if !strings.Contains(out, "@src.ts :L1-L5") {
		t.Errorf("hunk comment should report its range:\n%s", out)
	}
}

func TestFeedback_UntrackedFileComment(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "x\n", "init")
	tr.Untracked("src/utils/format.ts", "export const fmt = (s: string) => s.trim()\n")

	m := liveModel(t, tr)
	// Select the untracked file.
	for i, f := range m.files {
		if strings.HasSuffix(f.change.Path, "format.ts") {
			m.cursor = i
		}
	}
	if cmd := m.loadDiffCmd(true); cmd != nil {
		updated, _ := m.Update(cmd())
		m = updated.(Model)
	}
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	m = commentAt(t, m, LineAdded, "export const fmt = (s: string) => s.trim()", "add a test for this")

	out := review.FormatFeedback(m.session.Comments())
	if !strings.Contains(out, "@src/utils/format.ts :L1") {
		t.Errorf("feedback names the wrong file:\n%s", out)
	}
	// An untracked file is read straight off disk, so its line numbers are
	// the working tree's and the reference resolves — which is why the
	// payload carries no code. The excerpt is still on the comment, where
	// the degraded forms use it.
	if c := m.session.Comments()[0]; !strings.Contains(c.Excerpt, "+export const fmt") {
		t.Errorf("new file content should appear as added lines in the "+
			"excerpt: %q", c.Excerpt)
	}
}

func TestFeedback_MultipleCommentsAcrossFiles(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.AgentChangeset()
	m := liveModel(t, tr)
	updated, _ := m.updateFileListMode(key("enter"))
	m = updated.(Model)

	m.session.Add(review.Comment{File: "src/api/client.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "b"})
	m.session.Add(review.Comment{File: "src/auth/login.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "a"})

	out := review.FormatFeedback(m.session.Comments())
	if got := strings.Count(out, "Comment:"); got != 2 {
		t.Errorf("got %d comment sections, want 2", got)
	}
	// Files come out in a stable, sorted order.
	if strings.Index(out, "src/api/client.ts") > strings.Index(out, "src/auth/login.ts") {
		t.Errorf("files not in sorted order:\n%s", out)
	}
}
