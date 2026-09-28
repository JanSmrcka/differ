package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/theme"
)

// View composition and all rendering helpers.

func renderCard(t theme.Theme, title, content string, focused bool, w, h int) string {
	borderColor := lipgloss.Color(t.BorderFg)
	if focused {
		borderColor = lipgloss.Color(t.AccentFg)
	}
	bs := lipgloss.NewStyle().Foreground(borderColor)
	// The top border is "╭─" + title + fill + "╮", so the title has w-3 columns
	// to live in. A longer one must be truncated: clamping the fill instead
	// makes the border wider than the card, which ragged the whole layout.
	titleStr := ""
	if title != "" {
		titleStr = " " + truncateEnd(title, max(w-3, 0)) + " "
		if lipgloss.Width(titleStr) > w-1 {
			titleStr = ""
		}
	}
	topFill := w - lipgloss.Width(titleStr) - 1
	if topFill < 0 {
		topFill = 0
	}
	top := bs.Render("╭─" + titleStr + strings.Repeat("─", topFill) + "╮")
	lines := strings.Split(content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	cardBg := lipgloss.Color(t.CardBg)
	var rows []string
	for i := 0; i < h; i++ {
		line := lines[i]
		pad := w - lipgloss.Width(line)
		if pad > 0 {
			line += lipgloss.NewStyle().Background(cardBg).Render(strings.Repeat(" ", pad))
		}
		rows = append(rows, bs.Render("│")+line+bs.Render("│"))
	}
	bottom := bs.Render("╰" + strings.Repeat("─", w) + "╯")
	return lipgloss.JoinVertical(lipgloss.Left, top, strings.Join(rows, "\n"), bottom)
}

func (m Model) branchName() string {
	if m.repo == nil {
		return ""
	}
	return m.repo.BranchName()
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
	stagedRaw := "  "
	if f.change.Staged {
		stagedRaw = "● "
	}
	stats := fmt.Sprintf("+%d -%d", f.change.AddedLines, f.change.DeletedLines)
	if m.hasStaleComments(f.change.Path) {
		stats = staleMarker + " " + stats
	}
	name := filepath.Base(f.change.Path)
	if f.change.OldPath != "" {
		name = filepath.Base(f.change.OldPath) + " → " + filepath.Base(f.change.Path)
	}
	nameMaxW := fileListWidth - lipgloss.Width(stagedRaw) - lipgloss.Width(status) - 1 - lipgloss.Width(stats) - 1
	if nameMaxW < 1 {
		nameMaxW = 1
	}
	name = truncatePath(name, nameMaxW)
	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(fmt.Sprintf("%s%s %s %s", stagedRaw, status, name, stats))
	}
	staged := stagedRaw
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}
	line := fmt.Sprintf("%s%s %s %s", staged, m.styleStatus(status, f.change.Status), name, stats)
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

// hasStaleComments reports whether a file carries comments that no longer
// match the diff.
func (m Model) hasStaleComments(path string) bool {
	if m.session == nil {
		return false
	}
	for _, c := range m.session.CommentsFor(path) {
		if c.State == review.StateStale {
			return true
		}
	}
	return false
}

func (m Model) renderBranchList(height int) string {
	var b strings.Builder
	b.WriteString(m.renderBranchFilterBar())
	b.WriteByte('\n')
	list := m.activeBranches()
	itemH := height - 1
	if len(list) == 0 {
		b.WriteString(m.styles.FileItem.Width(fileListWidth).Render(m.styles.HelpDesc.Render("  no matches")))
		return b.String()
	}
	end := m.branchOffset + itemH
	if end > len(list) {
		end = len(list)
	}
	for i := m.branchOffset; i < end; i++ {
		b.WriteString(m.renderBranchItem(list[i], i == m.branchCursor, list[i] == m.currentBranch))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderBranchFilterBar() string {
	list := m.activeBranches()
	countStyled := m.styles.HelpDesc.Render(fmt.Sprintf("%d/%d", len(list), len(m.branches)))
	input := m.branchFilter.View()
	gap := fileListWidth - lipgloss.Width(input) - lipgloss.Width(countStyled) - 1
	if gap < 0 {
		gap = 0
	}
	return lipgloss.NewStyle().Width(fileListWidth).Render(input + strings.Repeat(" ", gap) + countStyled)
}

func (m Model) renderBranchItem(name string, selected, current bool) string {
	prefix := "  "
	if current {
		prefix = m.styles.StagedIcon.Render("* ")
	}
	line := prefix + truncatePath(name, fileListWidth-4)
	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(line)
	}
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

// truncateEnd shortens text to maxW columns, marking the cut with an ellipsis.
// Unlike truncatePath it keeps the start, which is what identifies a branch or
// a card.
func truncateEnd(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > maxW {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
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

// renderBar renders a single full-width line. Width() on its own wraps
// content that is too long onto a second row, which pushes the top of the
// layout off screen — MaxHeight(1) keeps it to one row.
func (m Model) renderBar(style lipgloss.Style, content string) string {
	return style.Width(m.width).MaxHeight(1).Render(content)
}

// helpPairs is the hint list for the current mode.
func (m Model) helpPairs() []struct{ key, desc string } {
	var pairs []struct{ key, desc string }
	switch m.mode {
	case modeDiff:
		pairs = []struct{ key, desc string }{{"j/k", "line"}, {"}/{", "hunk"}, {"d/u", "½ page"}, {"n/p", "file"}, {"r", "review"}, {"v", "split"}, {"tab", "stage"}, {"esc", "back"}, {"q", "quit"}}
	case modeReview:
		pairs = []struct{ key, desc string }{{"j/k", "line"}, {"}/{", "hunk"}, {"c", "comment"}, {"C", "hunk comment"}, {"x", "delete"}, {"s/S", "send one/all"}, {"n/p", "file"}, {"r", "exit review"}, {"q", "quit"}}
	case modeBranchPicker:
		pairs = []struct{ key, desc string }{{"type", "filter"}, {"↑/↓/^j/^k", "navigate"}, {"enter", "switch"}, {"^n", "new"}, {"esc", "clear/close"}}
	default:
		// Kept short enough to fit without truncation; the full keymap belongs
		// in a help overlay.
		pairs = []struct{ key, desc string }{{"j/k", "nav"}, {"enter", "diff"}, {"r", "review"}, {"tab", "stage"}, {"a", "all"}, {"c", "commit"}, {"b", "branch"}, {"e", "edit"}, {"P/F", "push/pull"}, {"q", "quit"}}
	}
	return pairs
}

func (m Model) helpContent(pairs []struct{ key, desc string }) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, m.styles.HelpKey.Render(p.key)+" "+m.styles.HelpDesc.Render(p.desc))
	}
	return " " + strings.Join(parts, "  ·  ")
}

func (m Model) renderCommitBar() string {
	return lipgloss.NewStyle().Width(m.width).Render(m.commitBarContent())
}

// renderCommentEditor shows the textarea plus what the two closing keys do.
func (m Model) renderCommentEditor() string {
	label := fmt.Sprintf(" comment · line %d ", m.draft.StartLine)
	if m.draft.EndLine > m.draft.StartLine {
		label = fmt.Sprintf(" comment · lines %d-%d ", m.draft.StartLine, m.draft.EndLine)
	}
	if m.editingID != "" {
		label = " edit" + label
	}
	head := m.renderBar(lipgloss.NewStyle(), m.styles.HelpKey.Render(label)+m.styles.HelpDesc.Render("· ctrl+s save · esc cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, head, m.commentInput.View())
}

func (m Model) renderBranchCreateBar() string {
	return lipgloss.NewStyle().Width(m.width).Render(m.branchCreateContent())
}

func (m Model) commitBarContent() string {
	prompt := m.styles.HelpKey.Render(" commit: ")
	if m.generatingMsg {
		return prompt + m.styles.HelpDesc.Render("generating...  esc cancel")
	}
	return prompt + m.commitInput.View() + "  " + m.styles.HelpDesc.Render("esc cancel · enter commit")
}

func (m Model) branchCreateContent() string {
	return m.styles.HelpKey.Render(" new branch: ") + m.branchInput.View() + "  " + m.styles.HelpDesc.Render("esc cancel · enter create")
}
