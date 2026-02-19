package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// diffLoadedMsg carries rendered diff content for the viewport.
type diffLoadedMsg struct {
	content string
	index   int
}

// Model is the main Bubble Tea model for the diff viewer.
type Model struct {
	repo       *git.Repo
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string

	mode     viewMode
	cursor   int
	prevCurs int // tracks cursor to detect change
	viewport viewport.Model
	width    int
	height   int
	ready    bool // viewport initialized
}

type fileItem struct {
	change    git.FileChange
	untracked bool
}

// NewModel creates the main diff viewer model.
func NewModel(
	repo *git.Repo,
	changes []git.FileChange,
	untracked []string,
	styles Styles,
	t theme.Theme,
	stagedOnly bool,
	ref string,
) Model {
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
	return Model{
		repo:       repo,
		files:      files,
		styles:     styles,
		theme:      t,
		stagedOnly: stagedOnly,
		ref:        ref,
		prevCurs:   -1, // force initial load
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadDiffCmd()
}

// Update dispatches messages to the appropriate mode handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case diffLoadedMsg:
		return m.handleDiffLoaded(msg)
	case tea.KeyMsg:
		switch m.mode {
		case modeFileList:
			return m.updateFileListMode(msg)
		case modeDiff:
			return m.updateDiffMode(msg)
		}
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	mainH := m.height - 2
	diffW := m.width - fileListWidth - 1 // 1 for border
	m.viewport = viewport.New(diffW, mainH)
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

func (m Model) updateFileListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	}
	// Load diff if cursor changed
	if m.cursor != m.prevCurs {
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd()
	}
	return m, nil
}

func (m Model) updateDiffMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "h", "left":
		m.mode = modeFileList
		return m, nil
	case "n":
		return m.nextFile()
	case "p":
		return m.prevFile()
	}
	// Forward to viewport for scrolling (j/k/d/u/g/G etc)
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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

// loadDiffCmd returns a command that loads and renders the diff for the current file.
func (m Model) loadDiffCmd() tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	idx := m.cursor
	f := m.files[idx]
	repo := m.repo
	styles := m.styles
	staged := f.change.Staged
	ref := m.ref
	diffW := m.width - fileListWidth - 1

	return func() tea.Msg {
		var content string
		if f.untracked {
			raw, err := repo.ReadFileContent(f.change.Path)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error reading file: " + err.Error())
			} else {
				content = RenderNewFile(raw, styles, diffW)
			}
		} else {
			raw, err := repo.DiffFile(f.change.Path, staged, ref)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error loading diff: " + err.Error())
			} else {
				parsed := ParseDiff(raw)
				content = RenderDiff(parsed, styles, diffW)
			}
		}
		return diffLoadedMsg{content: content, index: idx}
	}
}

// View renders the full UI.
func (m Model) View() string {
	if m.width == 0 || !m.ready {
		return ""
	}

	mainH := m.height - 2

	fileList := m.renderFileList(mainH)
	filePanel := lipgloss.NewStyle().Width(fileListWidth).Height(mainH).Render(fileList)

	border := m.renderBorder(mainH)
	diffPanel := lipgloss.NewStyle().Width(m.width - fileListWidth - 1).Height(mainH).Render(m.viewport.View())

	main := lipgloss.JoinHorizontal(lipgloss.Top, filePanel, border, diffPanel)
	statusBar := m.renderStatusBar()
	helpBar := m.renderHelpBar()

	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar, helpBar)
}

func (m Model) renderBorder(height int) string {
	border := strings.Repeat("│\n", height)
	if len(border) > 0 {
		border = border[:len(border)-1] // trim trailing newline
	}
	return m.styles.Border.Render(border)
}

func (m Model) renderFileList(height int) string {
	var b strings.Builder
	for i, f := range m.files {
		if i >= height {
			break
		}
		line := m.renderFileItem(f, i == m.cursor)
		b.WriteString(line)
		if i < len(m.files)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderFileItem(f fileItem, selected bool) string {
	status := statusIcon(f.change.Status)
	staged := "  "
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}

	statusStyled := m.styleStatus(status, f.change.Status)
	name := truncatePath(f.change.Path, fileListWidth-8)
	if f.change.OldPath != "" {
		name = truncatePath(f.change.OldPath+" → "+f.change.Path, fileListWidth-8)
	}

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

func statusIcon(s git.FileStatus) string {
	return string(s)
}

func (m Model) renderStatusBar() string {
	branch := m.repo.BranchName()
	info := fmt.Sprintf(" ⎇ %s", branch)

	fileInfo := ""
	if len(m.files) > 0 && m.cursor < len(m.files) {
		f := m.files[m.cursor]
		tag := ""
		if f.change.Staged {
			tag = " [staged]"
		}
		fileInfo = fmt.Sprintf("  %s%s", f.change.Path, tag)
	}

	right := fmt.Sprintf("%d files ", len(m.files))
	gap := m.width - lipgloss.Width(info) - lipgloss.Width(fileInfo) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := info + fileInfo + strings.Repeat(" ", gap) + right
	return m.styles.StatusBar.Width(m.width).Render(bar)
}

func (m Model) renderHelpBar() string {
	var pairs []struct{ key, desc string }
	switch m.mode {
	case modeDiff:
		pairs = []struct{ key, desc string }{
			{"j/k", "scroll"},
			{"d/u", "½ page"},
			{"n/p", "next/prev file"},
			{"esc", "back"},
			{"q", "quit"},
		}
	default:
		pairs = []struct{ key, desc string }{
			{"j/k", "navigate"},
			{"enter", "view diff"},
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
