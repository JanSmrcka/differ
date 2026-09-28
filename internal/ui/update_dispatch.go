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
	case feedbackSentMsg:
		return m.handleFeedbackSent(msg)
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
		return m.routeKey(msg)
	}
	return m, nil
}

func (m Model) routeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.lastDiffContent = msg.errContent
		m.viewport.SetContent(msg.errContent)
		if msg.resetScroll {
			m.viewport.GotoTop()
		}
		return m, nil
	}

	m.renderer = msg.renderer
	if m.session != nil {
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
	if filesEqual(m.files, msg.files) {
		return m, m.loadDiffCmd(false)
	}
	m.files = msg.files
	if m.cursor >= len(m.files) {
		m.cursor = max(0, len(m.files)-1)
	}
	m.prevCurs = -1
	m.lastDiffContent = ""
	if len(m.files) == 0 {
		m.viewport.SetContent("")
		return m, nil
	}
	return m, m.loadDiffCmd(true)
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
	return m, m.refreshFilesCmd()
}
