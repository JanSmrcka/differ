package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
)

// View composition and all rendering helpers.

func (m Model) branchName() string {
	if m.repo == nil {
		return ""
	}
	return m.repo.BranchName()
}

func (m Model) renderFileList(height int) string {
	end := min(m.fileOffset+max(height, 0), len(m.files))
	var b strings.Builder
	for i := m.fileOffset; i < end; i++ {
		b.WriteString(m.renderFileItem(m.files[i], i == m.cursor))
		if i < end-1 {
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
	// While reviewing, how far the user has got with the file takes the place
	// of the line counts: it is what they are navigating by, and 35 columns
	// does not hold both.
	right := stats
	if badge := m.reviewBadge(f.change.Path); badge != "" {
		right = badge
	}

	// The right-hand column sits against the right edge, so the eye can run
	// down it. The name is padded to whatever is left — after the panel's own
	// left padding, which the old arithmetic forgot, leaving names a column
	// too long and the column ragged.
	room := fileListWidth - filePanelPadding
	nameW := room - lipgloss.Width(stagedRaw) - lipgloss.Width(status) - 1 - lipgloss.Width(right) - 1
	name := padTo(truncatePath(m.displayName(f), max(nameW, 1)), max(nameW, 1))

	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(fmt.Sprintf("%s%s %s %s", stagedRaw, status, name, right))
	}

	staged := stagedRaw
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}
	line := fmt.Sprintf("%s%s %s %s", staged, m.styleStatus(status, f.change.Status), name, m.styleRight(f, right))
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

// displayName is how a file is named in the list: enough of its path to tell
// it apart from the others in the changeset, and both names for a rename.
func (m Model) displayName(f fileItem) string {
	short := m.shortNames()
	name := short[f.change.Path]
	if name == "" {
		name = filepath.Base(f.change.Path)
	}
	if f.change.OldPath != "" {
		return filepath.Base(f.change.OldPath) + " → " + name
	}
	return name
}

// shortNames is the disambiguated name for every file in the changeset,
// computed from the whole set because that is what decides how much of each
// path is needed.
func (m Model) shortNames() map[string]string {
	paths := make([]string, 0, len(m.files))
	for _, f := range m.files {
		paths = append(paths, f.change.Path)
	}
	return shortNames(paths)
}

// reviewBadge is the one-word review state of a file, or "" when there is
// nothing worth saying — outside review mode, or for a file nobody has looked
// at yet, which is the normal state and needs no badge.
func (m Model) reviewBadge(path string) string {
	if m.mode != modeReview || m.session == nil {
		return ""
	}
	var badge string
	switch m.session.FileStateOf(path) {
	case review.FileChanged:
		badge = "changed"
	case review.FileCommented:
		badge = plural(m.session.CountFor(path), "comment")
	case review.FileSent:
		badge = "sent"
	case review.FileViewed:
		badge = "·"
	default:
		return ""
	}
	// The badge takes the place of the line counts, which is where the stale
	// marker used to live — so it has to carry it, or a file whose comments no
	// longer match the diff stops being flagged in the list.
	if m.hasStaleComments(path) {
		badge = staleMarker + " " + badge
	}
	return badge
}

// styleRight colours the right-hand column: the additions and deletions carry
// the diff's own colours, and a review badge the comment colour, so the column
// says what kind of thing it is before it is read.
func (m Model) styleRight(f fileItem, text string) string {
	if m.reviewBadge(f.change.Path) != "" {
		return m.styles.CommentMeta.Render(text)
	}
	if text != fmt.Sprintf("+%d -%d", f.change.AddedLines, f.change.DeletedLines) {
		// Carries the stale marker; leave it in the marker's colour.
		return m.styles.CommentStale.Render(text)
	}
	return m.styles.StatusAdded.Render(fmt.Sprintf("+%d", f.change.AddedLines)) +
		" " + m.styles.StatusDeleted.Render(fmt.Sprintf("-%d", f.change.DeletedLines))
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
