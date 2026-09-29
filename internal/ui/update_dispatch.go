package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Update stays dispatcher-only; behavior lives in focused modules.
//
// Every message goes through fitViewport afterwards, not just key presses:
// an async result — a push finishing, feedback being sent, a commit landing —
// can add a status row and change how much room the panels have.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.dispatch(msg)
	if mm, ok := updated.(Model); ok {
		return mm.fitViewport(), cmd
	}
	return updated, cmd
}

func (m Model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tickMsg:
		return m.handleTick()
	case diffLoadedMsg:
		return m.handleDiffLoaded(msg)
	case filesRefreshedMsg:
		return m.handleFilesRefreshed(msg)
	case reanchorMsg:
		return m.handleReanchor(msg)
	case feedbackSentMsg:
		return m.handleFeedbackSent(msg)
	case editorPlanMsg:
		return m.handleEditorPlan(msg)
	case editorDoneMsg:
		return m.handleEditorDone(msg)
	case commitDoneMsg:
		return m.handleCommitDone(msg)
	case commitMsgGeneratedMsg:
		return m.handleCommitMsgGenerated(msg)
	case branchesLoadedMsg:
		return m.handleBranchesLoaded(msg)
	case branchSwitchedMsg:
		return m.handleBranchSwitched(msg)
	case branchCreatedMsg:
		return m.handleBranchCreated(msg)
	case upstreamStatusMsg:
		m.upstream = msg.info
		return m, nil
	case pushDoneMsg:
		return m.handlePushDone(msg)
	case pullDoneMsg:
		return m.handlePullDone(msg)
	case savePrefDoneMsg:
		if msg.err != nil {
			m.statusMsg = "config save failed"
		}
		return m, nil
	case tea.KeyMsg:
		// ctrl+c is documented as quitting immediately, so it is answered
		// before anything else can claim it. In the commit input and the
		// branch-name input the key reached the text field, which swallowed
		// it — bubbletea does not quit on ctrl+c by itself — and esc was the
		// only way out of either.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.routeKey(msg)
	}
	return m, nil
}

func (m Model) routeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// An open overlay owns the keyboard, whatever it is drawn over. The check
	// is outside the typing guard on purpose: an async message can switch the
	// mode underneath an overlay — the branch list arriving is enough — and
	// inside the guard every key then went into that mode's text input, which
	// left the overlay with no way to close.
	if m.showHelp || m.showHistory {
		switch msg.String() {
		case "?":
			m.showHelp, m.showHistory = !m.showHelp, false
			return m, nil
		case "H":
			// H closes the history, but does not open one from the help
			// overlay: unlike ?, it is not a global — it exists only in
			// review mode, and the file list's help does not list it.
			if m.showHistory {
				m.showHistory = false
				return m, nil
			}
		case "esc", "q":
			m.showHelp, m.showHistory = false, false
			return m, nil
		}
		// Everything else is swallowed, so a stray j does not scroll a diff
		// the user cannot see.
		return m, nil
	}

	if !m.typing() && msg.String() == "?" {
		m.showHelp = true
		return m, nil
	}

	switch m.mode {
	case modeFileList:
		return m.updateFileListMode(msg)
	case modeDiff:
		return m.updateDiffMode(msg)
	case modeCommit:
		return m.updateCommitMode(msg)
	case modeBranchPicker:
		return m.updateBranchMode(msg)
	case modeReview:
		return m.updateReviewMode(msg)
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport = viewport.New(m.diffWidth(), m.listHeight())
	m.lastDiffContent = ""
	m.ready = true
	// The scroll offsets are the only list state that depends on the height.
	// Growing the terminal past the whole changeset used to leave the window
	// where it was, putting the files above it back out of reach.
	m = m.clampFileScroll().clampBranchScroll()
	// Re-render at the new size without resetting: a resize (or a tmux pane
	// split) must not send the reviewer back to the top of the diff.
	return m, m.loadDiffCmd(false)
}

func (m Model) handleDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.index != m.cursor {
		return m, nil
	}
	if msg.renderer == nil {
		if msg.errContent == m.lastDiffContent {
			return m, nil
		}
		m.renderer = nil
		m.rendererPath = ""
		m.lastDiffContent = msg.errContent
		m.viewport.SetContent(msg.errContent)
		if msg.resetScroll {
			m.viewport.GotoTop()
		}
		return m, nil
	}

	m.renderer = msg.renderer
	m.rendererPath = m.currentFilePath()
	if m.session != nil {
		// Re-resolve this file's comments against the diff that just arrived,
		// so a comment follows its line or is marked stale — never left
		// pointing at whatever now occupies its old line number.
		m.session.Reanchor(m.currentFilePath(), diffLocations(msg.renderer.Parsed()))
		m.renderer.SetComments(m.session.CommentsFor(m.currentFilePath()))
	}
	// A new file, or the very first diff of the session, starts at the first
	// line worth reviewing. Everything else keeps the reviewer's position.
	if msg.resetScroll || !m.cursorPlaced {
		m.viewport.GotoTop()
		return m.setCursor(msg.renderer.Parsed().FirstCommentableLine()), nil
	}
	// A background refresh: keep the cursor where the user left it, clamped in
	// case the diff shrank underneath. applyContent leaves the viewport alone
	// when nothing changed, so polling cannot undo the user's own scrolling.
	m.diffCursor = clampCursor(m.diffCursor, m.renderer.LineCount())
	return m.applyContent(false), nil
}

func (m Model) handleFilesRefreshed(msg filesRefreshedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// git could not be read. An empty file list here means "unknown", not
		// "everything was committed", so nothing may be staled off it — a held
		// index.lock while the agent stages files would otherwise demote the
		// whole review with no way back.
		m.statusMsg = "refresh failed: " + msg.err.Error()
		return m, nil
	}
	m = m.noteChangedFiles(msg.keys)
	if filesEqual(m.files, msg.files) {
		return m, m.loadDiffCmd(false)
	}
	m.files = msg.files
	if m.session != nil {
		paths := make([]string, 0, len(m.files))
		for _, f := range m.files {
			paths = append(paths, f.change.Path)
		}
		m.session.StaleMissingFiles(paths)
	}
	if m.cursor >= len(m.files) {
		m.cursor = max(0, len(m.files)-1)
	}
	// The agent committing half its work shrinks the changeset under the
	// window, which would otherwise stay scrolled past the end and show an
	// empty panel.
	m = m.clampFileScroll()
	m.prevCurs = -1
	m.lastDiffContent = ""
	if len(m.files) == 0 {
		m.viewport.SetContent("")
		return m, nil
	}
	// The changeset moved, so comments on files that are not on screen need
	// re-anchoring too, not just the one being viewed.
	return m, tea.Batch(m.loadDiffCmd(true), m.reanchorAllCmd())
}

func (m Model) handleReanchor(msg reanchorMsg) (tea.Model, tea.Cmd) {
	if m.session == nil {
		return m, nil
	}
	for file, locations := range msg.locations {
		m.session.Reanchor(file, locations)
	}
	return m.refreshCommentMarks(), nil
}

func (m Model) handleCommitDone(msg commitDoneMsg) (tea.Model, tea.Cmd) {
	m.mode = modeFileList
	if msg.err != nil {
		m.statusMsg = "commit failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "committed!"
	m.commitInput.Reset()
	return m, m.refreshFilesCmd()
}

func (m Model) handleCommitMsgGenerated(msg commitMsgGeneratedMsg) (tea.Model, tea.Cmd) {
	m.generatingMsg = false
	if msg.err != nil {
		m.statusMsg = "ai msg failed: " + msg.err.Error()
		return m, nil
	}
	m.commitInput.SetValue(msg.message)
	m.commitInput.CursorEnd()
	return m, nil
}

func (m Model) handleBranchesLoaded(msg branchesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "branch list failed: " + msg.err.Error()
		return m, nil
	}
	if len(msg.branches) == 0 {
		m.statusMsg = "no branches"
		return m, nil
	}
	// An overlay belongs to the view it was opened over, and this is a
	// different view arriving in the background.
	m.showHelp, m.showHistory = false, false
	m.mode = modeBranchPicker
	m.branches = msg.branches
	m.currentBranch = msg.current
	m.branchCursor = 0
	m.branchOffset = 0
	for i, b := range m.branches {
		if b == msg.current {
			m.branchCursor = i
			break
		}
	}
	m.filteredBranches = nil
	m.branchFilter.Reset()
	m.branchFilter.Focus()
	return m, textinput.Blink
}

func (m Model) handleBranchSwitched(msg branchSwitchedMsg) (tea.Model, tea.Cmd) {
	m.mode = modeFileList
	m.filteredBranches = nil
	m.branchFilter.Reset()
	m.branchFilter.Blur()
	if msg.err != nil {
		m.statusMsg = "switch failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "switched to " + m.repo.BranchName()
	m.prevCurs = -1
	m.cursor = 0
	m = m.clampFileScroll()
	return m, m.refreshFilesCmd()
}

func (m Model) handleBranchCreated(msg branchCreatedMsg) (tea.Model, tea.Cmd) {
	m.branchCreating = false
	m.branchInput.Reset()
	if msg.err != nil {
		m.statusMsg = "create failed: " + msg.err.Error()
		return m, nil
	}
	m.mode = modeFileList
	m.statusMsg = "created & switched to " + msg.name
	m.prevCurs = -1
	m.cursor = 0
	m = m.clampFileScroll()
	return m, m.refreshFilesCmd()
}
