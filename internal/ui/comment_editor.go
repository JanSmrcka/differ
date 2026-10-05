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

// commentEditorWidth is how wide the text area is drawn: the modal's inner
// width, less the marker column the textarea draws itself.
//
// One function, called both where the editor is built and where it is drawn.
// The two used to disagree — View sized a value copy while the model kept the
// diff panel's width, 167 against the 82 on screen at 220 columns — so key
// handling and the render wrapped the text at different places.
func (m Model) commentEditorWidth() int {
	if m.height < commentModalMinHeight {
		return max(m.diffWidth()-4, 1) // the footer form, which is not in a box
	}
	return max(m.modalBoxWidth()-2*modalPadding-2, 1)
}

// startComment opens the editor for a new line comment, or reopens the
// comment already attached to this line.
func (m Model) startComment() (tea.Model, tea.Cmd) {
	if existing, ok := m.lineCommentAtCursor(); ok {
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
	m.commentInput = newCommentArea(m.commentEditorWidth(), body)
	// The editor takes several rows from the cards, so the viewport shrinks.
	m = m.resizeViewport()
	return m, textarea.Blink
}

func newCommentArea(width int, body string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "review comment..."
	ta.ShowLineNumbers = false
	ta.SetHeight(commentEditorHeight)
	// The width it is given, not that width less some margin. The margin used
	// to be taken here and again by the caller, so the text area wrapped four
	// columns before the box it sits in. commentEditorWidth is the one place
	// that arithmetic lives.
	if width > 0 {
		ta.SetWidth(width)
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
		m = m.closeEditor()
		return m, m.catchUp()
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
	return m.resizeViewport()
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
	m = m.closeEditor().persistReview()
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
	m = m.persistReview().refreshCommentMarks()
	return m, m.catchUp()
}

// commentAtCursor finds the comment the cursor points at, preferring one on
// exactly this line over one that merely spans it. Used where a hunk comment
// is still a sensible target — deleting and sending.
func (m Model) commentAtCursor() (review.Comment, bool) {
	exact, spanning, exactOK, spanningOK := m.commentsAtCursor()
	if exactOK {
		return exact, true
	}
	return spanning, spanningOK
}

// lineCommentAtCursor finds only a comment anchored to exactly this line.
//
// Opening the editor uses this so a hunk comment does not swallow the lines
// inside it: with only a hunk comment present, `c` on a line in that hunk
// starts a new line comment rather than reopening the hunk's.
func (m Model) lineCommentAtCursor() (review.Comment, bool) {
	exact, _, exactOK, _ := m.commentsAtCursor()
	return exact, exactOK
}

// commentsAtCursor splits the comments covering the cursor into the one
// anchored exactly here and the first that merely spans this line.
func (m Model) commentsAtCursor() (exact, spanning review.Comment, exactOK, spanningOK bool) {
	if m.session == nil || m.renderer == nil {
		return
	}
	addr, ok := m.renderer.Parsed().AddressOf(m.diffCursor)
	if !ok {
		return
	}
	side, line := sideAndLine(addr)

	for _, c := range m.session.CommentsFor(m.currentFilePath()) {
		if c.Side != side || line < c.StartLine || line > c.EndLine {
			continue
		}
		if c.StartLine == line && c.EndLine == line {
			if !exactOK {
				exact, exactOK = c, true
			}
			continue
		}
		if !spanningOK {
			spanning, spanningOK = c, true
		}
	}
	return
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
