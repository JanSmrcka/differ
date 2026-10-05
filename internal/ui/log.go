package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

type logMode int

const (
	logModeList logMode = iota
	logModeDiff
)

type logLoadedMsg struct {
	commits []git.Commit
}

type logDiffLoadedMsg struct {
	content string
	hash    string
}

// LogModel is the Bubble Tea model for the commit log browser.
type LogModel struct {
	repo     *git.Repo
	styles   Styles
	theme    theme.Theme
	tabWidth int
	commits  []git.Commit
	cursor   int
	// offset is the first commit drawn. Without it viewList drew
	// commits[:contentHeight] while the cursor could reach the hundredth, so
	// in a short terminal G selected a row nobody could see and enter opened a
	// commit whose hash appeared nowhere on screen.
	offset   int
	mode     logMode
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// NewLogModel creates the log browser model.
func NewLogModel(repo *git.Repo, styles Styles, t theme.Theme, tabWidth int) LogModel {
	return LogModel{repo: repo, styles: styles, theme: t, tabWidth: tabWidth}
}

func (m LogModel) Init() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		commits, _ := repo.Log(100)
		return logLoadedMsg{commits: commits}
	}
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(m.width-2, m.height-4)
		m.ready = true
	case logLoadedMsg:
		m.commits = msg.commits
	case logDiffLoadedMsg:
		m.viewport.SetContent(msg.content)
		m.viewport.GotoTop()
		m.mode = logModeDiff
	case tea.KeyMsg:
		switch m.mode {
		case logModeList:
			return m.updateList(msg)
		case logModeDiff:
			return m.updateDiff(msg)
		}
	}
	return m, nil
}

func (m LogModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.commits)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.commits)-1)
	case "enter":
		if len(m.commits) > 0 {
			return m, m.loadCommitDiff()
		}
	}
	return m, nil
}

func (m LogModel) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = logModeList
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m LogModel) loadCommitDiff() tea.Cmd {
	commit := m.commits[m.cursor]
	repo := m.repo
	styles := m.styles
	tabWidth := m.tabWidth
	t := m.theme
	width := m.width

	return func() tea.Msg {
		raw, err := repo.CommitDiff(commit.Hash)
		if err != nil {
			return logDiffLoadedMsg{content: "Error: " + err.Error(), hash: commit.Hash}
		}
		// Guess filename from diff headers for syntax highlighting
		content := renderCommitDiff(raw, styles, t, width, tabWidth)
		return logDiffLoadedMsg{content: content, hash: commit.Hash}
	}
}

// renderCommitDiff renders a full commit diff (may contain multiple files).
func renderCommitDiff(raw string, styles Styles, t theme.Theme, width, tabWidth int) string {
	render := func(parsed ParsedDiff, file string) string {
		r := NewDiffRenderer(parsed, file, styles, t, width)
		r.SetTabWidth(tabWidth)
		return r.Content(-1) + "\n"
	}

	var b strings.Builder
	// Split by file boundaries and render each section
	currentFile := ""
	var currentLines []string

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Flush previous file
			if len(currentLines) > 0 {
				b.WriteString(render(ParseDiff(strings.Join(currentLines, "\n")), currentFile))
			}
			currentFile = extractFilename(line)
			// Add file separator
			b.WriteString(styles.HeaderBar.Width(width).Render(" " + currentFile))
			b.WriteByte('\n')
			currentLines = []string{line}
		} else {
			currentLines = append(currentLines, line)
		}
	}
	// Flush last file
	if len(currentLines) > 0 {
		b.WriteString(render(ParseDiff(strings.Join(currentLines, "\n")), currentFile))
	}
	return b.String()
}

// extractFilename pulls the b/ path from "diff --git a/foo b/foo".
func extractFilename(diffHeader string) string {
	parts := strings.SplitN(diffHeader, " b/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func (m LogModel) View() string {
	if !m.ready {
		return ""
	}

	switch m.mode {
	case logModeDiff:
		return m.viewDiff()
	default:
		return m.viewList()
	}
}

// The log browser shares the main view's frame: a header, a rule, the
// content, a rule, then the command bar. It used to draw bordered cards and
// its own status bar, so `differ` and `differ log` looked like two programs.

// logChromeRows is the header plus the two rules — the same three the main
// view spends, so the arithmetic matches.
const logChromeRows = 3

// contentHeight is the room left for the list or the diff.
func (m LogModel) contentHeight() int { return max(m.height-logChromeRows-1, 0) }

func (m LogModel) frame(body string) string {
	rows := strings.Split(body, "\n")
	h := m.contentHeight()
	for len(rows) < h {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = padTo(rows[i], m.width)
	}
	rule := m.styles.Chrome.Render(strings.Repeat(horizontalRule, max(m.width, 0)))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		rule,
		strings.Join(rows[:h], "\n"),
		rule,
		m.renderLogBar(),
	)
}

// renderHeader mirrors the main view's: identity on the left, a summary of
// what is on show on the right.
func (m LogModel) renderHeader() string {
	name := m.styles.HeaderName.Render(" differ")
	ctx := m.styles.HeaderBranch.Render("log")
	identity := name + m.styles.Chrome.Render(headerSep) + ctx

	summary := m.styles.HeaderMeta.Render(plural(len(m.commits), "commit") + " ")
	if m.mode == logModeDiff && m.cursor < len(m.commits) {
		c := m.commits[m.cursor]
		summary = m.styles.HeaderMeta.Render(c.Short + headerSep + c.Author + " ")
	}

	gap := m.width - lipgloss.Width(identity) - lipgloss.Width(summary)
	if gap < 1 {
		return lipgloss.NewStyle().Width(m.width).MaxHeight(1).Render(identity)
	}
	return lipgloss.NewStyle().Width(m.width).MaxHeight(1).
		Render(identity + strings.Repeat(" ", gap) + summary)
}

func (m LogModel) viewList() string {
	m = m.clampLogScroll()
	var rows []string
	end := min(m.offset+m.contentHeight(), len(m.commits))
	for i := m.offset; i < end; i++ {
		rows = append(rows, m.renderCommitLine(m.commits[i], i == m.cursor))
	}
	return m.frame(strings.Join(rows, "\n"))
}

// clampLogScroll keeps the cursor inside the drawn window, and the window
// inside the list.
//
// The same arithmetic as clampFileScroll, for the same reason: a list longer
// than the panel needs somewhere to say which part of it is on screen.
func (m LogModel) clampLogScroll() LogModel {
	h := m.contentHeight()
	if h <= 0 || len(m.commits) == 0 {
		m.offset = 0
		return m
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if max := len(m.commits) - h; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
	return m
}

func (m LogModel) renderCommitLine(c git.Commit, selected bool) string {
	// Three columns, not three words with two spaces between them: the hash
	// is fixed width, the date sits against the right edge and the subject
	// takes what is left. The changed-file list is built the same way, so the
	// eye can run down the right-hand column on either screen.
	room := max(m.width-lipgloss.Width(c.Short)-4, 0)
	subjectRoom := max(room-lipgloss.Width(c.Date), 0)
	subject := truncateEnd(c.Subject, subjectRoom)

	line := m.styles.Accent.Render(c.Short) + "  " +
		padTo(subject, subjectRoom) + " " + m.styles.HelpDesc.Render(c.Date) + " "
	if selected {
		// The same marker the changed-file list and the diff use. Bold and a
		// foreground were the only difference before, so stripped of colour
		// the selected row was byte-identical to the others — and the one
		// column of padding on Selected made the list jitter as the cursor
		// moved, because the unselected rows had none.
		return m.styles.Selected.Width(m.width).Render(cursorMarker + line)
	}
	return lipgloss.NewStyle().Width(m.width).Render(" " + line)
}

func (m LogModel) viewDiff() string {
	return m.frame(m.viewport.View())
}

// renderLogBar is the log browser's command bar, built the same way as the
// main view's so the two read alike.
func (m LogModel) renderLogBar() string {
	s := surfaceLogList
	if m.mode == logModeDiff {
		s = surfaceLogDiff
	}
	var parts []string
	for _, b := range surfaceKeymap(s) {
		if !b.Bar {
			continue
		}
		parts = append(parts, m.styles.HelpKey.Render(b.label())+" "+m.styles.HelpDesc.Render(b.Desc))
	}
	return lipgloss.NewStyle().Width(m.width).MaxHeight(1).
		Render(" " + strings.Join(parts, barSeparator))
}
