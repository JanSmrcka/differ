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
)

const fileListWidth = 35
const pollInterval = 2 * time.Second

type tickMsg time.Time

// Messages
type diffLoadedMsg struct {
	content string
	index   int
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

// Model is the main Bubble Tea model for the diff viewer.
type Model struct {
	repo       *git.Repo
	cfg        config.Config
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string

	mode        viewMode
	cursor      int
	prevCurs    int
	viewport    viewport.Model
	commitInput textinput.Model
	statusMsg    string
	generatingMsg bool
	splitDiff    bool
	width        int
	height       int
	ready        bool
	SelectedFile string // set on "open in editor" action, read after Run()
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
	files := buildFileItems(changes, untracked)

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

func buildFileItems(changes []git.FileChange, untracked []string) []fileItem {
	var files []fileItem
	for _, c := range changes {
		files = append(files, fileItem{change: c})
	}
	for _, path := range untracked {
		files = append(files, fileItem{
			change:    git.FileChange{Path: path, Status: git.StatusUntracked},
			untracked: true,
		})
	}
	return files
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
	cmds := []tea.Cmd{m.loadDiffCmd(), tickCmd()}
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
		}
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport = viewport.New(m.diffWidth(), m.contentHeight())
	m.ready = true
	return m, m.loadDiffCmd()
}

func (m Model) handleDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.index == m.cursor {
		m.viewport.SetContent(msg.content)
		m.viewport.GotoTop()
	}
	return m, nil
}

func (m Model) handleFilesRefreshed(msg filesRefreshedMsg) (tea.Model, tea.Cmd) {
	if filesEqual(m.files, msg.files) {
		return m, m.loadDiffCmd()
	}
	m.files = msg.files
	if m.cursor >= len(m.files) {
		m.cursor = max(0, len(m.files)-1)
	}
	m.prevCurs = -1
	if len(m.files) == 0 {
		m.viewport.SetContent("")
		return m, nil
	}
	return m, m.loadDiffCmd()
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

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) handleTick() (tea.Model, tea.Cmd) {
	if m.mode == modeCommit || m.generatingMsg {
		return m, tickCmd()
	}
	return m, tea.Batch(m.refreshFilesCmd(), tickCmd())
}

// File list mode
func (m Model) updateFileListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = ""
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
	case "v":
		m.splitDiff = !m.splitDiff
		m.prevCurs = -1
		return m, tea.Batch(m.loadDiffCmd(), m.saveSplitPrefCmd())
	}
	if m.cursor != m.prevCurs {
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd()
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
	case "tab":
		return m.toggleStage()
	case "v":
		m.splitDiff = !m.splitDiff
		m.prevCurs = -1
		return m, tea.Batch(m.loadDiffCmd(), m.saveSplitPrefCmd())
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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
		return m, m.loadDiffCmd()
	}
	return m, nil
}

func (m Model) prevFile() (tea.Model, tea.Cmd) {
	if m.cursor > 0 {
		m.cursor--
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd()
	}
	return m, nil
}

// Commands
func (m Model) loadDiffCmd() tea.Cmd {
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
		return diffLoadedMsg{content: content, index: idx}
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
		return filesRefreshedMsg{files: buildFileItems(files, untracked)}
	}
}

func (m Model) buildRefreshedFiles() filesRefreshedMsg {
	files, _ := m.repo.ChangedFiles(m.stagedOnly, m.ref)
	var untracked []string
	if !m.stagedOnly && m.ref == "" {
		untracked, _ = m.repo.UntrackedFiles()
	}
	return filesRefreshedMsg{files: buildFileItems(files, untracked)}
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

	fileList := m.renderFileList(contentH)
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
	name := filepath.Base(f.change.Path)
	if f.change.OldPath != "" {
		name = filepath.Base(f.change.OldPath) + " → " + filepath.Base(f.change.Path)
	}
	name = truncatePath(name, fileListWidth-10)

	line := fmt.Sprintf("%s%s %s", staged, statusStyled, name)
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
			{"esc", "back"},
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
			{"c", "commit"},
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
