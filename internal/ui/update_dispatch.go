package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
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
	case agentsLoadedMsg:
		return m.handleAgentsLoaded(msg)
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
	// Same reason as the theme picker: it owns j/k and enter, so it is
	// answered before the overlays that only close.
	if m.showAgents {
		mm, cmd := m.agentPickerKey(msg.String())
		return mm, cmd
	}

	if m.showHelp || m.showHistory || m.showProblem {
		return m.readingOverlayKey(msg.String())
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
		case "H":
			// Global, because what was sent is about the whole changeset, not
			// the file the cursor happens to be on.
			m.showHistory = true
			return m, nil
		case "t":
			return m.openThemePicker()
		case agentKey:
			mm, cmd := m.openAgentPicker()
			return mm, cmd
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
	}
	return m, nil
}

// readingOverlayKey answers keys while help, history or the problem is open.
// The three switch between each other, and everything else is swallowed, so
// a stray j does not scroll a diff the user cannot see.
func (m Model) readingOverlayKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "?":
		m.showHelp, m.showHistory, m.showProblem = !m.showHelp, false, false
	case "!":
		m.showProblem, m.showHelp, m.showHistory = !m.showProblem, false, false
	case "H":
		m.showHistory, m.showHelp, m.showProblem = !m.showHistory, false, false
	case "t":
		return m.openThemePicker()
	case agentKey:
		mm, cmd := m.openAgentPicker()
		return mm, cmd
	case "esc", "q":
		m.showHelp, m.showHistory, m.showProblem = false, false, false
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
	// A resize must not re-read the file while its diff is being held: that is
	// the content swap the hold exists to prevent, and it would take the
	// notice saying it had happened with it. Re-render the parse in hand at
	// the new width instead.
	// holdsTheDiff, not diffStale alone: between a refresh installing new keys
	// and the load it batched landing, diffStale is briefly true in the plain
	// diff and the file list too — and a resize there would keep the old
	// content in a mode where R is unbound.
	if m.holdsTheDiff() && m.diffStale() {
		return m, m.rerenderCmd()
	}
	return m, m.loadDiffCmd(false)
}

func (m Model) handleDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	// Matched by path, with the index as a tiebreak for the two entries a
	// staged-and-unstaged file has. The index alone identified nothing: it
	// addresses m.files, which every refresh replaces — so a load in flight
	// when the changeset reordered was installed against whatever had taken
	// its slot, and the panel showed one file's diff under another's name.
	if msg.path != "" && msg.path != m.currentFilePath() {
		return m, nil
	}
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

	// A re-render carries the content that was on screen when it was asked
	// for. If something has replaced that content since — an explicit reload
	// landing first — the re-render is stale and installing it would undo the
	// reload and put the notice back. The key it was built from says so; no
	// separate sequence is needed, and a counter on the model would not work
	// anyway, because loadDiffCmd is called on a copy in `return m, cmd`.
	if msg.rerender && msg.key != m.rendererKey {
		return m, nil
	}

	m.renderer = msg.renderer
	m.rendererPath = m.currentFilePath()
	// What this diff was built from. Staleness is the difference between this
	// and the file's key now, so recording it here is the only place the
	// notice is ever cleared — and it cannot be forgotten, because forgetting
	// it means having no renderer either.
	m = m.noteRenderedDiff(msg.key)
	if m.session != nil {
		// Re-resolve this file's comments against the diff that just arrived,
		// so a comment follows its line or is marked stale — never left
		// pointing at whatever now occupies its old line number.
		//
		// Not for a re-render: that is the same content at a new width, and
		// the parse it carries still contains the lines the agent has since
		// deleted. Re-anchoring against it restored every comment the refresh
		// had just marked stale, so #44 would send one on the first press
		// quoting code that is gone. The comments themselves still need
		// installing on the new renderer.
		if !msg.rerender {
			m.session.Reanchor(m.currentFilePath(), review.Anchored{
				Locations: diffLocations(msg.renderer.Parsed()),
				Locate:    m.locateFor(review.SideNew),
			})
		}
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
		//
		// The fingerprint is not stored either, so the next probe still finds
		// a difference and tries again. It used to be stored when the refresh
		// was asked for, which turned one unreadable moment into a screen that
		// never updated again.
		return m.fail("refresh", msg.err), nil
	}
	// Two refreshes can be out at once — a probe's and one from staging a file
	// — and they can land in either order. The older one must not win: it
	// would install state the repository has moved past, and its fingerprint
	// would then claim the screen was current. m.fileKeys is the worse half of
	// that: #43 would mark files as changed under the reviewer purely because
	// an older set of hashes arrived last.
	if msg.seq < m.installedSeq {
		return m, nil
	}
	m.installedSeq = msg.seq
	if msg.fingerprint != "" {
		m.repoFingerprint = msg.fingerprint
	}
	// The file on screen moved under a reviewer. The refresh itself still
	// lands — the list, its marks and the review counts are all current — but
	// the diff they are reading is not swapped out from under them, and a
	// half-written comment against it keeps its anchor. They are told instead;
	// reloading is theirs to ask for.
	//
	// Asked before noteChangedFiles, which installs the new keys: after it the
	// comparison is the new keys against themselves.
	//
	// Decided here, once, and outside every branch below. It used to live
	// inside the filesEqual arm — and filesEqual compares the diff's added and
	// deleted line counts, so any edit that changed those fell through to a
	// reload that reset the cursor while the notice still claimed the diff was
	// being held. Adding one line was enough.
	hold := m.holdsTheDiff() && m.currentFileMoved(msg.keys)
	// Which half of the entry the cursor is on, for followCursorTo. A path
	// with both staged and unstaged changes is two entries, and moving the
	// cursor to the wrong one made R swap in the other half's diff — on the
	// one action that is supposed to be safe.
	wasStaged, wasPath := false, ""
	if m.cursor < len(m.files) {
		wasStaged = m.files[m.cursor].change.Staged
		wasPath = m.files[m.cursor].change.Path
	}
	m = m.noteChangedFiles(msg.keys, hold)
	if filesEqual(m.files, msg.files) {
		// Re-anchoring happens whether or not the display is held. It is what
		// marks a comment stale, and #44 refuses to send a stale comment
		// without a second press — so skipping it left a comment about a line
		// the agent had already replaced looking pending and sendable. The
		// first fix put this in the other arm of this same if, and a
		// same-length replacement — the commonest edit there is — took this
		// one.
		if hold {
			return m, m.reanchorCmd(true)
		}
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
	// The cursor is an index, and the list it indexes has just been replaced.
	// A file sorting earlier joining the changeset shifts everything below it,
	// so the index has to follow the entry it was on — otherwise the list
	// highlights one file while the panel shows another.
	//
	// Only when the cursor and the renderer still agree. They disagree exactly
	// when the user has navigated and the new diff has not landed yet — and
	// dragging the cursor back to the renderer's file there undid the
	// navigation silently, because the load for the file they asked for is
	// then discarded by the index guard in handleDiffLoaded.
	if m.rendererPath == wasPath {
		m = m.followCursorTo(m.rendererPath, wasStaged)
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
	//
	// Holding the display must not hold the bookkeeping. Re-anchoring is what
	// marks a comment stale, and #44 refuses to send a stale one without a
	// second press — so skipping it here meant a comment about a line the
	// agent had already deleted stayed "pending" and went out on the first
	// press, carrying an excerpt of code that no longer exists.
	if hold {
		return m, m.reanchorCmd(true)
	}
	// resetScroll only in the file list. The changeset changing is not a
	// reason to send a reader back to the top of the file they are reading —
	// and with an agent working, another file changing is the common case, not
	// the rare one.
	return m, tea.Batch(m.loadDiffCmd(m.mode != modeDiff), m.reanchorAllCmd())
}

func (m Model) handleReanchor(msg reanchorMsg) (tea.Model, tea.Cmd) {
	if m.session == nil {
		return m, nil
	}
	for file, anchored := range msg.locations {
		m.session.Reanchor(file, anchored)
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
	refresh := m.nextRefresh()
	return m, refresh
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
	m.showAgents = false
	var restore tea.Cmd
	if m.showThemes {
		m, restore = m.cancelTheme()
	}
	m.mode = modeBranchPicker
	m.branches = msg.branches
	m.currentBranch = msg.current
	m.branchDirty = msg.dirty
	m.branchConfirm = ""
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
	// The header reads currentBranch rather than asking git on every frame, so
	// the one place that knows the branch moved has to say so.
	m.currentBranch = m.repo.BranchName()
	m.statusMsg = "switched to " + m.currentBranch
	m.prevCurs = -1
	m.cursor = 0
	m = m.clampFileScroll()
	refresh := m.nextRefresh()
	return m, refresh
}

func (m Model) handleBranchCreated(msg branchCreatedMsg) (tea.Model, tea.Cmd) {
	m.branchCreating = false
	m.branchInput.Reset()
	if msg.err != nil {
		return m.fail("creating the branch", msg.err), nil
	}
	m.mode = modeFileList
	m.currentBranch = msg.name
	m.statusMsg = "created & switched to " + msg.name
	m.prevCurs = -1
	m.cursor = 0
	m = m.clampFileScroll()
	refresh := m.nextRefresh()
	return m, refresh
}

// fitInputsToPanels sizes the two text inputs from the panels they sit in.
//
// Not only on resize: listWidth() depends on whether there are any files, so
// the last file going away widens the file-list panel with no WindowSizeMsg
// involved at all — and the branch filter, sized once at 112 columns, then
// wrapped onto a second row and ate a row of the branch list.
func (m Model) fitInputsToPanels() Model {
	m.branchFilter.Width = max(m.listWidth()-8, 1)
	// On the model, not in View: a width set on View's copy never reached the
	// input, which at width 0 never scrolls — a long message ran out of the
	// box and was clipped with the cursor in it.
	m.commitInput.Width = m.commitInputWidth()
	// The comment textarea is sized when the editor opens and was never
	// resized after. lipgloss.JoinVertical pads every row of the frame to the
	// widest one, so a textarea left at its old width made the whole frame
	// that wide — thirty rows of 149 columns in a 120-column terminal.
	if m.commenting {
		m.commentInput.SetWidth(m.commentEditorWidth())
	}
	return m
}
