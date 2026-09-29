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
	case repoProbedMsg:
		return m.handleRepoProbed(msg)
	case upstreamStatusMsg:
		m.upstream = msg.info
		return m, nil
	case pushDoneMsg:
		return m.handlePushDone(msg)
	case pullDoneMsg:
		return m.handlePullDone(msg)
	case savePrefDoneMsg:
		if msg.err != nil {
			m = m.fail("saving the config", msg.err)
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
	// The theme picker owns its keys entirely, including j/k and enter, so it
	// is answered before the overlays that only close.
	if m.showThemes {
		mm, cmd, _ := m.themePickerKey(msg.String())
		return mm, cmd
	}

	if m.showHelp || m.showHistory || m.showProblem {
		switch msg.String() {
		case "?":
			m.showHelp, m.showHistory, m.showProblem = !m.showHelp, false, false
			return m, nil
		case "!":
			m.showProblem, m.showHelp, m.showHistory = !m.showProblem, false, false
			return m, nil
		case "t":
			return m.openThemePicker()
		case "H":
			// H closes the history, but does not open one from the help
			// overlay: unlike ?, it is not a global — it exists only in
			// review mode, and the file list's help does not list it.
			if m.showHistory {
				m.showHistory = false
				return m, nil
			}
		case "esc", "q":
			m.showHelp, m.showHistory, m.showProblem = false, false, false
			return m, nil
		}
		// Everything else is swallowed, so a stray j does not scroll a diff
		// the user cannot see.
		return m, nil
	}

	if !m.typing() {
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "!":
			// Global, because a failure can come from anything — including a
			// mode that has since been left.
			m.showProblem = true
			return m, nil
		case "t":
			return m.openThemePicker()
		}
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
	m = m.fitInputsToPanels()
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
	// Built under a theme that is no longer in use. Two git diffs can be in
	// flight — a preview and the reload that cancelled it — and they do not
	// finish in the order they started.
	if msg.themeGen != m.themeGen {
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
		return m.fail("refresh", msg.err), nil
	}
	m = m.noteChangedFiles(msg.keys)
	if filesEqual(m.files, msg.files) {
		return m, m.loadDiffCmd(false)
	}
	m.files = msg.files
	// The file count decides whether the layout is one panel or two, so the
	// panels can change width here without any resize.
	m = m.fitInputsToPanels()
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
		return m.fail("commit", msg.err), nil
	}
	m.statusMsg = "committed!"
	m.commitInput.Reset()
	return m, m.refreshFilesCmd()
}

func (m Model) handleCommitMsgGenerated(msg commitMsgGeneratedMsg) (tea.Model, tea.Cmd) {
	m.generatingMsg = false
	if msg.err != nil {
		return m.fail("the commit message", msg.err), nil
	}
	m.commitInput.SetValue(msg.message)
	m.commitInput.CursorEnd()
	return m, nil
}

func (m Model) handleBranchesLoaded(msg branchesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.fail("listing branches", msg.err), nil
	}
	if len(msg.branches) == 0 {
		m.statusMsg = "no branches"
		return m, nil
	}
	// An overlay belongs to the view it was opened over, and this is a
	// different view arriving in the background.
	m.showHelp, m.showHistory, m.showProblem = false, false, false
	// The theme picker is closed through cancelTheme rather than by clearing
	// the flag: it is the only overlay that changes the session as you move
	// through it, so dropping it without restoring left the user in a theme
	// they never confirmed, with esc no longer able to undo it.
	var restore tea.Cmd
	if m.showThemes {
		m, restore = m.cancelTheme()
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
	// The current branch can be past the panel height — git branch is sorted,
	// so anything past the twenty-somethingth — and the picker then opened
	// with no visible selection at all.
	m = m.clampBranchScroll()
	return m, tea.Batch(restore, textinput.Blink)
}

func (m Model) handleBranchSwitched(msg branchSwitchedMsg) (tea.Model, tea.Cmd) {
	m.mode = modeFileList
	m.filteredBranches = nil
	m.branchFilter.Reset()
	m.branchFilter.Blur()
	if msg.err != nil {
		return m.fail("switch", msg.err), nil
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
		return m.fail("creating the branch", msg.err), nil
	}
	m.mode = modeFileList
	m.statusMsg = "created & switched to " + msg.name
	m.prevCurs = -1
	m.cursor = 0
	m = m.clampFileScroll()
	return m, m.refreshFilesCmd()
}

// fitInputsToPanels sizes the two text inputs from the panels they sit in.
//
// Not only on resize: listWidth() depends on whether there are any files, so
// the last file going away widens the file-list panel with no WindowSizeMsg
// involved at all — and the branch filter, sized once at 112 columns, then
// wrapped onto a second row and ate a row of the branch list.
func (m Model) fitInputsToPanels() Model {
	m.branchFilter.Width = max(m.listWidth()-8, 1)
	// The comment textarea is sized when the editor opens and was never
	// resized after. lipgloss.JoinVertical pads every row of the frame to the
	// widest one, so a textarea left at its old width made the whole frame
	// that wide — thirty rows of 149 columns in a 120-column terminal.
	if m.commenting {
		m.commentInput.SetWidth(max(m.diffWidth()-4, 1))
	}
	return m
}
