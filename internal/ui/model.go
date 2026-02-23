package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

type viewMode int

const (
	modeFileList viewMode = iota
	modeDiff
	modeCommit
	modeBranchPicker
)

const fileListWidth = 35
const pollInterval = 2 * time.Second

type tickMsg time.Time

// Messages
type diffLoadedMsg struct {
	content     string
	index       int
	resetScroll bool
}

type filesRefreshedMsg struct {
	files []fileItem
}

type commitDoneMsg struct {
	err error
}

type commitMsgGeneratedMsg struct {
	message string
	err     error
}

type branchesLoadedMsg struct {
	branches []string
	current  string
	err      error
}

type branchSwitchedMsg struct {
	err error
}

type upstreamStatusMsg struct {
	info git.UpstreamInfo
}

type pushDoneMsg struct {
	err error
}

type pullDoneMsg struct {
	err error
}

// Model is the main Bubble Tea model for the diff viewer.
type Model struct {
	repo       *git.Repo
	cfg        config.Config
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string

	mode          viewMode
	cursor        int
	prevCurs      int
	viewport      viewport.Model
	commitInput   textinput.Model
	statusMsg     string
	generatingMsg bool
	splitDiff     bool
	width         int
	height        int
	ready         bool
	SelectedFile  string // set on "open in editor" action, read after Run()

	lastDiffContent string

	// Branch picker state
	branches      []string
	branchCursor  int
	branchOffset  int
	currentBranch string

	// Push/pull state
	upstream    git.UpstreamInfo
	pushConfirm bool
}

type fileItem struct {
	change    git.FileChange
	untracked bool
}

// NewModel creates the main diff viewer model.
func NewModel(
	repo *git.Repo,
	cfg config.Config,
	changes []git.FileChange,
	untracked []string,
	styles Styles,
	t theme.Theme,
	stagedOnly bool,
	ref string,
) Model {
	files := buildFileItems(repo, changes, untracked)

	ti := textinput.New()
	ti.Placeholder = "commit message..."
	ti.CharLimit = 200

	return Model{
		repo:        repo,
		cfg:         cfg,
		files:       files,
		styles:      styles,
		theme:       t,
		stagedOnly:  stagedOnly,
		ref:         ref,
		splitDiff:   cfg.SplitDiff,
		prevCurs:    -1,
		commitInput: ti,
	}
}

func buildFileItems(repo *git.Repo, changes []git.FileChange, untracked []string) []fileItem {
	var files []fileItem
	for _, c := range changes {
		files = append(files, fileItem{change: c})
	}
	for _, path := range untracked {
		added := 0
		if repo != nil {
			raw, err := repo.ReadFileContent(path)
			if err == nil {
				added = countLines(raw)
			}
		}
		files = append(files, fileItem{
			change:    git.FileChange{Path: path, Status: git.StatusUntracked, AddedLines: added, DeletedLines: 0},
			untracked: true,
		})
	}
	return files
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		count++
	}
	return count
}

func filesEqual(a, b []fileItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StartInCommitMode sets the model to open directly in commit mode.
func (m *Model) StartInCommitMode() {
	m.mode = modeCommit
	m.commitInput.Focus()
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadDiffCmd(true), m.fetchUpstreamStatusCmd(), tickCmd()}
	if m.mode == modeCommit {
		cmds = append(cmds, textinput.Blink)
	}
	return tea.Batch(cmds...)
}

// Update dispatches messages to the appropriate mode handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tickMsg:
		return m.handleTick()
	case diffLoadedMsg:
		return m.handleDiffLoaded(msg)
	case filesRefreshedMsg:
		return m.handleFilesRefreshed(msg)
	case commitDoneMsg:
		return m.handleCommitDone(msg)
	case commitMsgGeneratedMsg:
		return m.handleCommitMsgGenerated(msg)
	case branchesLoadedMsg:
		return m.handleBranchesLoaded(msg)
	case branchSwitchedMsg:
		return m.handleBranchSwitched(msg)
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
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport = viewport.New(m.diffWidth(), m.contentHeight())
	m.ready = true
	return m, m.loadDiffCmd(true)
}

func (m Model) handleDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.index != m.cursor {
		return m, nil
	}
	if msg.content == m.lastDiffContent {
		return m, nil
	}
	m.lastDiffContent = msg.content
	m.viewport.SetContent(msg.content)
	if msg.resetScroll {
		m.viewport.GotoTop()
	}
	return m, nil
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

func (m Model) enterBranchMode() (tea.Model, tea.Cmd) {
	repo := m.repo
	return m, func() tea.Msg {
		branches, err := repo.ListBranches()
		if err != nil {
			return branchesLoadedMsg{err: err}
		}
		current := repo.BranchName()
		return branchesLoadedMsg{branches: branches, current: current}
	}
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
	return m, nil
}

func (m Model) handleBranchSwitched(msg branchSwitchedMsg) (tea.Model, tea.Cmd) {
	m.mode = modeFileList
	if msg.err != nil {
		m.statusMsg = "switch failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "switched to " + m.repo.BranchName()
	m.prevCurs = -1
	m.cursor = 0
	return m, m.refreshFilesCmd()
}

func (m Model) handlePushDone(msg pushDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "push failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "pushed!"
	return m, m.fetchUpstreamStatusCmd()
}

func (m Model) handlePullDone(msg pullDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "pull failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "pulled!"
	return m, tea.Batch(m.refreshFilesCmd(), m.fetchUpstreamStatusCmd())
}

func (m Model) fetchUpstreamStatusCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		return upstreamStatusMsg{info: repo.UpstreamStatus()}
	}
}

func (m Model) pushCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		return pushDoneMsg{err: repo.Push()}
	}
}

func (m Model) pullCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		return pullDoneMsg{err: repo.Pull()}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) handleTick() (tea.Model, tea.Cmd) {
	if m.mode == modeCommit || m.mode == modeBranchPicker || m.generatingMsg {
		return m, tickCmd()
	}
	return m, tea.Batch(m.refreshFilesCmd(), m.fetchUpstreamStatusCmd(), tickCmd())
}

// File list mode
func (m Model) updateFileListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = ""

	// Handle push confirmation before clearing state
	if msg.String() == "P" {
		if m.pushConfirm {
			m.pushConfirm = false
			m.statusMsg = "pushing..."
			return m, m.pushCmd()
		}
		if m.upstream.Upstream == "" {
			m.statusMsg = "no upstream configured"
			return m, nil
		}
		m.pushConfirm = true
		m.statusMsg = "press P again to push to " + m.upstream.Upstream
		return m, nil
	}
	m.pushConfirm = false

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.files)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.files)-1)
	case "enter", "l", "right":
		m.mode = modeDiff
		return m, nil
	case "e":
		if m.cursor < len(m.files) {
			m.SelectedFile = m.files[m.cursor].change.Path
		}
		return m, tea.Quit
	case "tab":
		return m.toggleStage()
	case "a":
		return m.stageAll()
	case "c":
		return m.enterCommitMode()
	case "b":
		return m.enterBranchMode()
	case "v":
		m.splitDiff = !m.splitDiff
		m.prevCurs = -1
		m.lastDiffContent = ""
		return m, tea.Batch(m.loadDiffCmd(true), m.saveSplitPrefCmd())
	case "F":
		if m.upstream.Upstream == "" {
			m.statusMsg = "no upstream configured"
			return m, nil
		}
		m.statusMsg = "pulling..."
		return m, m.pullCmd()
	}
	if m.cursor != m.prevCurs {
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

// Diff view mode
func (m Model) updateDiffMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "h", "left":
		m.mode = modeFileList
		return m, nil
	case "n":
		return m.nextFile()
	case "p":
		return m.prevFile()
	case "e":
		if m.cursor < len(m.files) {
			m.SelectedFile = m.files[m.cursor].change.Path
		}
		return m, tea.Quit
	case "b":
		return m.enterBranchMode()
	case "tab":
		return m.toggleStage()
	case "v":
		m.splitDiff = !m.splitDiff
		m.prevCurs = -1
		m.lastDiffContent = ""
		return m, tea.Batch(m.loadDiffCmd(true), m.saveSplitPrefCmd())
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// Branch picker mode
func (m Model) updateBranchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "b":
		m.mode = modeFileList
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.branchCursor < len(m.branches)-1 {
			m.branchCursor++
		}
	case "k", "up":
		if m.branchCursor > 0 {
			m.branchCursor--
		}
	case "g":
		m.branchCursor = 0
		m.branchOffset = 0
	case "G":
		m.branchCursor = max(0, len(m.branches)-1)
	case "enter":
		if m.branchCursor >= len(m.branches) {
			return m, nil
		}
		selected := m.branches[m.branchCursor]
		if selected == m.currentBranch {
			m.mode = modeFileList
			return m, nil
		}
		repo := m.repo
		return m, func() tea.Msg {
			return branchSwitchedMsg{err: repo.CheckoutBranch(selected)}
		}
	}
	// Keep cursor in visible window
	h := m.contentHeight()
	if h > 0 {
		if m.branchCursor < m.branchOffset {
			m.branchOffset = m.branchCursor
		} else if m.branchCursor >= m.branchOffset+h {
			m.branchOffset = m.branchCursor - h + 1
		}
	}
	return m, nil
}

// Commit mode
func (m Model) updateCommitMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeFileList
		m.commitInput.Reset()
		return m, nil
	case "enter":
		msg := m.commitInput.Value()
		if strings.TrimSpace(msg) == "" {
			m.statusMsg = "empty commit message"
			return m, nil
		}
		return m, m.commitCmd(msg)
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

// Stage/unstage operations
func (m Model) toggleStage() (tea.Model, tea.Cmd) {
	if m.stagedOnly || m.ref != "" || len(m.files) == 0 {
		return m, nil
	}
	f := m.files[m.cursor]
	repo := m.repo
	path := f.change.Path

	return m, func() tea.Msg {
		if f.change.Staged {
			_ = repo.UnstageFile(path)
		} else {
			_ = repo.StageFile(path)
		}
		return m.buildRefreshedFiles()
	}
}

func (m Model) stageAll() (tea.Model, tea.Cmd) {
	if m.stagedOnly || m.ref != "" {
		return m, nil
	}
	repo := m.repo
	return m, func() tea.Msg {
		_ = repo.StageAll()
		return m.buildRefreshedFiles()
	}
}

func (m Model) enterCommitMode() (tea.Model, tea.Cmd) {
	if m.ref != "" {
		return m, nil
	}
	hasStaged := false
	for _, f := range m.files {
		if f.change.Staged {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		m.statusMsg = "no staged files"
		return m, nil
	}
	m.mode = modeCommit
	m.generatingMsg = true
	m.statusMsg = "generating commit message..."
	m.commitInput.Focus()
	return m, tea.Batch(textinput.Blink, m.generateCommitMsgCmd())
}

func (m Model) nextFile() (tea.Model, tea.Cmd) {
	if m.cursor < len(m.files)-1 {
		m.cursor++
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) prevFile() (tea.Model, tea.Cmd) {
	if m.cursor > 0 {
		m.cursor--
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

// Commands
func (m Model) loadDiffCmd(resetScroll bool) tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	idx := m.cursor
	f := m.files[idx]
	repo := m.repo
	styles := m.styles
	t := m.theme
	staged := f.change.Staged
	ref := m.ref
	diffW := m.diffWidth()
	filename := f.change.Path
	splitMode := m.splitDiff && diffW >= minSplitWidth

	return func() tea.Msg {
		var content string
		if f.untracked {
			raw, err := repo.ReadFileContent(filename)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error: " + err.Error())
			} else if splitMode {
				content = RenderNewFileSplit(raw, filename, styles, t, diffW)
			} else {
				content = RenderNewFile(raw, filename, styles, t, diffW)
			}
		} else {
			raw, err := repo.DiffFile(filename, staged, ref)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error: " + err.Error())
			} else {
				parsed := ParseDiff(raw)
				if splitMode {
					content = RenderSplitDiff(parsed, filename, styles, t, diffW)
				} else {
					content = RenderDiff(parsed, filename, styles, t, diffW)
				}
			}
		}
		return diffLoadedMsg{content: content, index: idx, resetScroll: resetScroll}
	}
}

func (m Model) refreshFilesCmd() tea.Cmd {
	repo := m.repo
	stagedOnly := m.stagedOnly
	ref := m.ref

	return func() tea.Msg {
		files, _ := repo.ChangedFiles(stagedOnly, ref)
		var untracked []string
		if !stagedOnly && ref == "" {
			untracked, _ = repo.UntrackedFiles()
		}
		return filesRefreshedMsg{files: buildFileItems(repo, files, untracked)}
	}
}

func (m Model) buildRefreshedFiles() filesRefreshedMsg {
	files, _ := m.repo.ChangedFiles(m.stagedOnly, m.ref)
	var untracked []string
	if !m.stagedOnly && m.ref == "" {
		untracked, _ = m.repo.UntrackedFiles()
	}
	return filesRefreshedMsg{files: buildFileItems(m.repo, files, untracked)}
}

type savePrefDoneMsg struct{ err error }

func (m Model) saveSplitPrefCmd() tea.Cmd {
	cfg := m.cfg
	split := m.splitDiff
	return func() tea.Msg {
		cfg.SplitDiff = split
		return savePrefDoneMsg{err: config.Save(cfg)}
	}
}

func (m Model) commitCmd(message string) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		err := repo.Commit(message)
		return commitDoneMsg{err: err}
	}
}

const defaultCommitMsgCmd = "claude -p"
const defaultCommitMsgPrompt = "Write a concise git commit message (one line, no quotes, use conventional commit prefixes like feat:, fix:, chore:, refactor: etc when appropriate) for this diff:"

func (m Model) generateCommitMsgCmd() tea.Cmd {
	repo := m.repo
	cfg := m.cfg
	return func() tea.Msg {
		diff, err := repo.StagedDiff()
		if err != nil {
			return commitMsgGeneratedMsg{err: fmt.Errorf("git diff: %w", err)}
		}
		if strings.TrimSpace(diff) == "" {
			return commitMsgGeneratedMsg{err: fmt.Errorf("empty staged diff")}
		}
		const maxDiff = 8000
		if len(diff) > maxDiff {
			diff = diff[:maxDiff] + "\n... (truncated)"
		}

		promptPrefix := defaultCommitMsgPrompt
		if cfg.CommitMsgPrompt != "" {
			promptPrefix = cfg.CommitMsgPrompt
		}
		prompt := promptPrefix + "\n\n" + diff

		cmdStr := defaultCommitMsgCmd
		if cfg.CommitMsgCmd != "" {
			cmdStr = cfg.CommitMsgCmd
		}
		parts := strings.Fields(cmdStr)
		args := append(parts[1:], prompt)
		cmd := exec.Command(parts[0], args...)
		out, err := cmd.Output()
		if err != nil {
			return commitMsgGeneratedMsg{err: fmt.Errorf("%s: %w", parts[0], err)}
		}
		msg := strings.TrimSpace(string(out))
		return commitMsgGeneratedMsg{message: msg}
	}
}

// Layout: header(1) + content + status(1) + help(1) = height
func (m Model) contentHeight() int { return m.height - 3 }
func (m Model) diffWidth() int     { return m.width - fileListWidth - 1 }

const (
	minWidth  = 60
	minHeight = 10
)

// View renders the full UI.
func (m Model) View() string {
	if m.width == 0 || !m.ready {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal too small (%dx%d). Minimum: %dx%d",
			m.width, m.height, minWidth, minHeight)
	}

	header := m.renderHeader()
	contentH := m.contentHeight()

	var fileList string
	if m.mode == modeBranchPicker {
		fileList = m.renderBranchList(contentH)
	} else {
		fileList = m.renderFileList(contentH)
	}
	filePanel := lipgloss.NewStyle().
		Width(fileListWidth).Height(contentH).
		Render(fileList)

	border := m.renderBorder(contentH)
	diffPanel := lipgloss.NewStyle().
		Width(m.diffWidth()).Height(contentH).
		Render(m.viewport.View())

	main := lipgloss.JoinHorizontal(lipgloss.Top, filePanel, border, diffPanel)
	statusBar := m.renderStatusBar()

	if m.mode == modeCommit {
		return lipgloss.JoinVertical(lipgloss.Left, header, main, statusBar, m.renderCommitBar())
	}
	helpBar := m.renderHelpBar()
	return lipgloss.JoinVertical(lipgloss.Left, header, main, statusBar, helpBar)
}

func (m Model) renderHeader() string {
	branch := m.repo.BranchName()
	left := m.styles.HeaderBar.Render(" " + branch)

	mode := ""
	if m.ref != "" {
		mode = m.styles.Accent.Render("  ref:" + m.ref)
	} else if m.stagedOnly {
		mode = m.styles.Accent.Render("  staged only")
	}

	right := ""
	if len(m.files) > 0 && m.cursor < len(m.files) {
		f := m.files[m.cursor]
		name := f.change.Path
		if f.change.Staged {
			name += " [staged]"
		}
		right = name
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mode) - lipgloss.Width(right) - 1
	if gap < 0 {
		gap = 0
	}

	bar := left + mode + strings.Repeat(" ", gap) + right + " "
	return m.styles.StatusBar.Width(m.width).Render(bar)
}

func (m Model) renderBorder(height int) string {
	border := strings.Repeat("│\n", height)
	if len(border) > 0 {
		border = border[:len(border)-1]
	}
	style := m.styles.Border
	if m.mode == modeDiff {
		style = m.styles.BorderFocus
	}
	return style.Render(border)
}

func (m Model) renderFileList(height int) string {
	var b strings.Builder
	for i, f := range m.files {
		if i >= height {
			break
		}
		b.WriteString(m.renderFileItem(f, i == m.cursor))
		if i < len(m.files)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderFileItem(f fileItem, selected bool) string {
	status := string(f.change.Status)
	staged := "  "
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}

	statusStyled := m.styleStatus(status, f.change.Status)
	stats := fmt.Sprintf("+%d -%d", f.change.AddedLines, f.change.DeletedLines)
	name := filepath.Base(f.change.Path)
	if f.change.OldPath != "" {
		name = filepath.Base(f.change.OldPath) + " → " + filepath.Base(f.change.Path)
	}
	nameMaxW := fileListWidth - lipgloss.Width(staged) - lipgloss.Width(status) - 1 - lipgloss.Width(stats) - 1
	if nameMaxW < 1 {
		nameMaxW = 1
	}
	name = truncatePath(name, nameMaxW)

	line := fmt.Sprintf("%s%s %s %s", staged, statusStyled, name, stats)
	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(line)
	}
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

func (m Model) renderBranchList(height int) string {
	var b strings.Builder
	end := m.branchOffset + height
	if end > len(m.branches) {
		end = len(m.branches)
	}
	for i := m.branchOffset; i < end; i++ {
		branch := m.branches[i]
		b.WriteString(m.renderBranchItem(branch, i == m.branchCursor, branch == m.currentBranch))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderBranchItem(name string, selected, current bool) string {
	prefix := "  "
	if current {
		prefix = m.styles.StagedIcon.Render("* ")
	}
	name = truncatePath(name, fileListWidth-4)
	line := prefix + name
	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(line)
	}
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

func truncatePath(path string, maxW int) string {
	if lipgloss.Width(path) <= maxW {
		return path
	}
	for lipgloss.Width(path) > maxW-1 && len(path) > 1 {
		path = path[1:]
	}
	return "…" + path
}

func (m Model) styleStatus(icon string, status git.FileStatus) string {
	switch status {
	case git.StatusModified:
		return m.styles.StatusModified.Render(icon)
	case git.StatusAdded:
		return m.styles.StatusAdded.Render(icon)
	case git.StatusDeleted:
		return m.styles.StatusDeleted.Render(icon)
	case git.StatusRenamed:
		return m.styles.StatusRenamed.Render(icon)
	case git.StatusUntracked:
		return m.styles.StatusUntracked.Render(icon)
	default:
		return icon
	}
}

func (m Model) renderStatusBar() string {
	stagedCount := 0
	for _, f := range m.files {
		if f.change.Staged {
			stagedCount++
		}
	}

	left := fmt.Sprintf(" %d staged  %d files", stagedCount, len(m.files))
	if m.upstream.Upstream != "" && (m.upstream.Ahead > 0 || m.upstream.Behind > 0) {
		left += fmt.Sprintf("  ↑%d ↓%d", m.upstream.Ahead, m.upstream.Behind)
	}
	if m.splitDiff {
		left += "  split"
	}
	if m.statusMsg != "" {
		left += "  " + m.statusMsg
	}

	return m.styles.StatusBar.Width(m.width).Render(left)
}

func (m Model) renderHelpBar() string {
	var pairs []struct{ key, desc string }
	switch m.mode {
	case modeDiff:
		pairs = []struct{ key, desc string }{
			{"j/k", "scroll"},
			{"d/u", "½ page"},
			{"n/p", "next/prev"},
			{"v", "split"},
			{"tab", "stage"},
			{"e", "edit"},
			{"b", "branches"},
			{"esc", "back"},
			{"q", "quit"},
		}
	case modeBranchPicker:
		pairs = []struct{ key, desc string }{
			{"j/k", "navigate"},
			{"enter", "switch"},
			{"esc", "cancel"},
			{"q", "quit"},
		}
	default:
		pairs = []struct{ key, desc string }{
			{"j/k", "navigate"},
			{"enter", "view diff"},
			{"v", "split"},
			{"tab", "stage/unstage"},
			{"a", "stage all"},
			{"e", "edit"},
			{"b", "branches"},
			{"c", "commit"},
			{"P", "push"},
			{"F", "pull"},
			{"q", "quit"},
		}
	}

	var parts []string
	for _, p := range pairs {
		parts = append(parts,
			m.styles.HelpKey.Render(p.key)+" "+m.styles.HelpDesc.Render(p.desc))
	}
	bar := " " + strings.Join(parts, "  ·  ")
	return lipgloss.NewStyle().Width(m.width).Render(bar)
}

func (m Model) renderCommitBar() string {
	prompt := m.styles.HelpKey.Render(" commit: ")
	if m.generatingMsg {
		hint := m.styles.HelpDesc.Render("generating...  esc cancel")
		return lipgloss.NewStyle().Width(m.width).Render(prompt + hint)
	}
	input := m.commitInput.View()
	esc := "  " + m.styles.HelpDesc.Render("esc cancel · enter commit")
	return lipgloss.NewStyle().Width(m.width).Render(prompt + input + esc)
}
