package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
)

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == '\n' {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		updated, _ := m.updateReviewMode(msg)
		m = updated.(Model)
	}
	return m
}

func reviewOnAddedLine(t *testing.T) Model {
	t.Helper()
	m := reviewModel(t, "multi_hunk")
	return cursorOn(t, m, LineAdded, "  const user = await getUser(id)")
}

func TestCommentEditor_COpensEditor(t *testing.T) {
	m := reviewOnAddedLine(t)

	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	if !m.commenting {
		t.Fatal("c should open the comment editor")
	}
	if m.draft.StartLine != 2 {
		t.Errorf("draft anchored at line %d, want 2", m.draft.StartLine)
	}
}

func TestCommentEditor_TypingAndSaving(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	m = typeText(t, m, "keep this awaited")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if m.commenting {
		t.Error("saving should close the editor")
	}
	got := m.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("got %d comments, want 1", len(got))
	}
	if got[0].Body != "keep this awaited" {
		t.Errorf("Body = %q", got[0].Body)
	}
	if got[0].State != review.StatePending {
		t.Errorf("State = %v, want pending", got[0].State)
	}
}

func TestCommentEditor_SupportsMultilineBodies(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	m = typeText(t, m, "first line\nsecond line")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	body := m.session.CommentsFor("src.ts")[0].Body
	if !strings.Contains(body, "first line") || !strings.Contains(body, "second line") {
		t.Errorf("multiline body lost: %q", body)
	}
	if !strings.Contains(body, "\n") {
		t.Errorf("body should contain a newline: %q", body)
	}
}

func TestCommentEditor_EscCancelsWithoutSaving(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "discard me")

	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.commenting {
		t.Error("esc should close the editor")
	}
	if n := m.session.CountFor("src.ts"); n != 0 {
		t.Errorf("cancelled comment was saved anyway (%d comments)", n)
	}
	if m.mode != modeReview {
		t.Errorf("esc in the editor should stay in review mode, got %v", m.mode)
	}
}

func TestCommentEditor_EmptyBodyIsNotSaved(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if n := m.session.CountFor("src.ts"); n != 0 {
		t.Errorf("empty comment was saved (%d comments)", n)
	}
	if !m.commenting {
		t.Error("an empty save should keep the editor open rather than silently dropping it")
	}
}

func TestCommentEditor_NavigationKeysGoToTheTextareaNotTheCursor(t *testing.T) {
	m := reviewOnAddedLine(t)
	cursorBefore := m.diffCursor
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	m = typeText(t, m, "jjjj")

	if m.diffCursor != cursorBefore {
		t.Errorf("typing in the editor moved the diff cursor from %d to %d", cursorBefore, m.diffCursor)
	}
	if !strings.Contains(m.commentInput.Value(), "jjjj") {
		t.Errorf("text did not reach the editor: %q", m.commentInput.Value())
	}
}

func TestCommentEditor_ShiftCCommentsTheHunk(t *testing.T) {
	m := reviewOnAddedLine(t)

	updated, _ := m.updateReviewMode(key("C"))
	m = updated.(Model)
	m = typeText(t, m, "whole hunk")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	got := m.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("got %d comments, want 1", len(got))
	}
	if got[0].StartLine != 1 || got[0].EndLine != 5 {
		t.Errorf("hunk comment covers %d-%d, want 1-5", got[0].StartLine, got[0].EndLine)
	}
}

func TestCommentEditor_MultipleCommentsCoexist(t *testing.T) {
	m := reviewOnAddedLine(t)

	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "first")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	m = cursorOn(t, m, LineAdded, "  persist(data)")
	updated, _ = m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "second")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	got := m.session.CommentsFor("src.ts")
	if len(got) != 2 {
		t.Fatalf("got %d comments, want 2", len(got))
	}
	if got[0].Body != "first" || got[1].Body != "second" {
		t.Errorf("bodies = %q, %q", got[0].Body, got[1].Body)
	}
}

func TestCommentEditor_EditingAnExistingComment(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "before")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	// c on a line that already carries a comment edits it.
	updated, _ = m.updateReviewMode(key("c"))
	m = updated.(Model)
	if !m.commenting {
		t.Fatal("c should reopen the existing comment")
	}
	if m.commentInput.Value() != "before" {
		t.Errorf("editor should be pre-filled with the existing body, got %q", m.commentInput.Value())
	}

	m = typeText(t, m, " and after")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	got := m.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("editing created a second comment (%d total)", len(got))
	}
	if got[0].Body != "before and after" {
		t.Errorf("Body = %q", got[0].Body)
	}
}

func TestCommentEditor_DeleteComment(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "delete me")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	updated, _ = m.updateReviewMode(key("x"))
	m = updated.(Model)

	if n := m.session.CountFor("src.ts"); n != 0 {
		t.Errorf("comment not deleted (%d remain)", n)
	}
}

func TestCommentEditor_DeleteWithNoCommentIsHarmless(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("x"))
	m = updated.(Model)
	if m.session.CountFor("src.ts") != 0 {
		t.Error("unexpected comment")
	}
}

func TestCommentEditor_CommentsSurviveFileNavigation(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "stays")
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	// Leave review, come back.
	updated, _ = m.updateReviewMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	updated, _ = m.Update(key("r"))
	m = updated.(Model)

	if n := m.session.CountFor("src.ts"); n != 1 {
		t.Errorf("comment lost across navigation (%d remain)", n)
	}
}

func TestCommentEditor_IsVisibleWhileOpen(t *testing.T) {
	m := reviewOnAddedLine(t)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m = typeText(t, m, "visible text")

	view := m.View()
	if !strings.Contains(view, "visible text") {
		t.Errorf("the comment being typed is not on screen:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+s") {
		t.Errorf("the editor should say how to save:\n%s", view)
	}
	if !strings.Contains(view, "esc") {
		t.Errorf("the editor should say how to cancel:\n%s", view)
	}
}

func TestReviewHelpBar_MentionsCommentKeys(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	help := m.renderHintBar()
	for _, want := range []string{"comment", "delete"} {
		if !strings.Contains(help, want) {
			t.Errorf("review help missing %q: %q", want, help)
		}
	}
}
