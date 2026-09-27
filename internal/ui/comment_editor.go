package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
)

// The comment editor: a multiline textarea over the diff.
//
// While it is open every key belongs to the editor, so typing "j" writes a
// "j" rather than moving the diff cursor.

const commentEditorHeight = 4

// startComment opens the editor for a new line comment, or reopens the
// comment already attached to this line.
func (m Model) startComment() (tea.Model, tea.Cmd) {
	if existing, ok := m.commentAtCursor(); ok {
		return m.openEditor(existing, existing.ID, existing.Body)
	}
	draft, ok := m.buildLineComment()
	if !ok {
		m.statusMsg = "nothing to comment on here"
		return m, nil
	}
	return m.openEditor(draft, "", "")
}

// startHunkComment opens the editor for a comment covering the whole hunk.
func (m Model) startHunkComment() (tea.Model, tea.Cmd) {
	draft, ok := m.buildHunkComment()
	if !ok {
		m.statusMsg = "no hunk here"
		return m, nil
	}
	return m.openEditor(draft, "", "")
}

func (m Model) openEditor(draft review.Comment, editingID, body string) (tea.Model, tea.Cmd) {
	m.commenting = true
	m.draft = draft
	m.editingID = editingID
	m.commentInput = newCommentArea(m.diffWidth(), body)
	return m, textarea.Blink
}

func newCommentArea(width int, body string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "review comment..."
	ta.ShowLineNumbers = false
	ta.SetHeight(commentEditorHeight)
	if width > 8 {
		ta.SetWidth(width - 4)
	}
	if body != "" {
		ta.SetValue(body)
	}
	ta.Focus()
	ta.CursorEnd()
	return ta
}

// updateCommentEditor routes keys to the textarea, except the two that close
// it.
func (m Model) updateCommentEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.closeEditor(), nil
	case tea.KeyCtrlS:
		return m.saveComment()
	case tea.KeyCtrlC:
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.commentInput, cmd = m.commentInput.Update(msg)
	return m, cmd
}

func (m Model) closeEditor() Model {
	m.commenting = false
	m.editingID = ""
	m.draft = review.Comment{}
	m.commentInput.Reset()
	m.commentInput.Blur()
	return m
}

func (m Model) saveComment() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.commentInput.Value())
	if body == "" {
		// Keep the editor open rather than quietly discarding the action.
		m.statusMsg = "comment is empty — esc to cancel"
		return m, nil
	}
	if m.session == nil {
		m.session = review.NewSession()
	}

	if m.editingID != "" {
		m.session.UpdateBody(m.editingID, body)
		m.statusMsg = "comment updated"
	} else {
		c := m.draft
		c.Body = body
		m.session.Add(c)
		m.statusMsg = "comment added"
	}
	m = m.closeEditor()
	return m.refreshCommentMarks(), nil
}

// deleteCommentAtCursor removes the comment anchored to the current line.
func (m Model) deleteCommentAtCursor() (tea.Model, tea.Cmd) {
	c, ok := m.commentAtCursor()
	if !ok {
		return m, nil
	}
	m.session.Remove(c.ID)
	m.statusMsg = "comment deleted"
	return m.refreshCommentMarks(), nil
}

// commentAtCursor finds the comment anchored to the line under the cursor.
func (m Model) commentAtCursor() (review.Comment, bool) {
	if m.session == nil || m.renderer == nil {
		return review.Comment{}, false
	}
	addr, ok := m.renderer.Parsed().AddressOf(m.diffCursor)
	if !ok {
		return review.Comment{}, false
	}
	side, line := sideAndLine(addr)
	for _, c := range m.session.CommentsFor(m.currentFilePath()) {
		if c.Side == side && line >= c.StartLine && line <= c.EndLine {
			return c, true
		}
	}
	return review.Comment{}, false
}

// refreshCommentMarks re-renders the diff so comment markers and bodies match
// the session. Called after anything that changes the comment set, and after
// a new diff is loaded.
func (m Model) refreshCommentMarks() Model {
	if m.renderer == nil {
		return m
	}
	var cs []review.Comment
	if m.session != nil {
		cs = m.session.CommentsFor(m.currentFilePath())
	}
	m.renderer.SetComments(cs)
	return m.syncCursorViewport()
}
