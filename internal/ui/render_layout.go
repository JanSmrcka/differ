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

// branchName is the branch on the model, never a question for git.
//
// It used to call repo.BranchName(), which is a synchronous `git rev-parse` —
// inside View, so every rendered frame started a process and blocked ~8 ms on
// it. That is one per keypress, and it is why #45's "idle sessions issue
// approximately no git subprocesses" did not hold however cheap the probe got.
// The name changes about once a session; it is read when the session starts and
// whenever a branch is switched or created.
func (m Model) branchName() string {
	return m.currentBranch
}

// renderFileList draws the visible window of the changeset.
//
// It reads listHeight itself rather than taking a height: the scroll offset is
// clamped against listHeight, so a caller passing anything else would render a
// window the clamping never agreed to.
func (m Model) renderFileList() string {
	if len(m.files) == 0 {
		// A blank panel reads as a bug. Say what is true instead.
		return m.renderEmptyState()
	}
	height := m.listHeight()
	end := min(m.fileOffset+max(height, 0), len(m.files))

	// Once for the list, not once per row. The disambiguation looks at every
	// path in the changeset, so computing it inside the row renderer made a
	// frame cost rows × files — 9 ms and 39 MB of garbage at a thousand files,
	// on every keypress.
	short := m.shortNames()

	var b strings.Builder
	for i := m.fileOffset; i < end; i++ {
		b.WriteString(m.renderFileItem(m.files[i], i == m.cursor, short))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderFileItem(f fileItem, selected bool, short map[string]string) string {
	status := string(f.change.Status)
	stagedRaw := "  "
	if f.change.Staged {
		stagedRaw = "● "
	}
	right := m.rightColumn(f)

	// The right-hand column sits against the right edge, so the eye can run
	// down it. The name is padded to whatever is left — after the panel's own
	// left padding, which the old arithmetic forgot, leaving names a column
	// too long and the column ragged.
	room := m.listWidth() - filePanelPadding
	nameW := max(room-lipgloss.Width(stagedRaw)-lipgloss.Width(status)-1-lipgloss.Width(right.text)-1, 1)
	name := padTo(truncatePath(m.displayName(f, short), nameW), nameW)

	if selected {
		return m.styles.FileSelected.Width(m.listWidth()).Render(fmt.Sprintf("%s%s %s %s", stagedRaw, status, name, right.text))
	}

	staged := stagedRaw
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}
	line := fmt.Sprintf("%s%s %s %s", staged, m.styleStatus(status, f.change.Status), name, right.render(m.styles))
	return m.styles.FileItem.Width(m.listWidth()).Render(line)
}

// displayName is how a file is named in the list: enough of its path to tell
// it apart from the others in the changeset, and both names for a rename.
func (m Model) displayName(f fileItem, short map[string]string) string {
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

// rightColumn is the right-hand column of a row, decided once.
//
// Text and colour are worked out together because the colour depends on what
// kind of thing the text is. They used to be separate, and the second half
// re-derived the first by comparing the string it had just been handed against
// a freshly formatted one — which also meant asking the session the same
// questions three times per row.
type rightColumn struct {
	// stale is the "!" prefix, kept apart so it can keep the stale colour
	// whatever the rest of the column is.
	stale bool
	text  string
	// badge says the text is review state rather than line counts.
	badge          bool
	added, deleted int
}

func (m Model) rightColumn(f fileItem) rightColumn {
	c := rightColumn{
		stale:   m.hasStaleComments(f.change.Path),
		added:   f.change.AddedLines,
		deleted: f.change.DeletedLines,
	}
	// While reviewing, how far the user has got with the file takes the place
	// of the line counts: it is what they are navigating by, and 35 columns
	// does not hold both.
	if badge := m.reviewBadge(f.change.Path); badge != "" {
		c.badge, c.text = true, badge
	} else {
		c.text = fmt.Sprintf("+%d -%d", c.added, c.deleted)
	}
	if c.stale {
		c.text = staleMarker + " " + c.text
	}
	return c
}

// render colours the column: the additions and deletions in the diff's own
// colours, review state in the comment colour, and the stale marker always in
// the stale colour — so the column says what kind of thing it is before it is
// read.
func (c rightColumn) render(styles Styles) string {
	var out string
	if c.stale {
		out = styles.CommentStale.Render(staleMarker + " ")
	}
	if c.badge {
		return out + styles.CommentMeta.Render(strings.TrimPrefix(c.text, staleMarker+" "))
	}
	return out + styles.StatusAdded.Render(fmt.Sprintf("+%d", c.added)) +
		" " + styles.StatusDeleted.Render(fmt.Sprintf("-%d", c.deleted))
}

// reviewBadge is the one-word review state of a file, or "" when there is
// nothing worth saying — outside review mode, or for a file nobody has looked
// at yet, which is the normal state and needs no badge.
func (m Model) reviewBadge(path string) string {
	if m.mode != modeReview || m.session == nil {
		return ""
	}
	switch m.session.FileStateOf(path) {
	case review.FileChanged:
		return "changed"
	case review.FileCommented:
		return plural(m.session.CountFor(path), "comment")
	case review.FileSent:
		return "sent"
	case review.FileViewed:
		// A word, not a glyph: "·" already means rendered whitespace inside a
		// diff and separates the parts of the status bar, and three meanings
		// for one mark on one screen is two too many.
		return "read"
	default:
		return ""
	}
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
		b.WriteString(m.styles.FileItem.Width(m.listWidth()).Render(m.styles.HelpDesc.Render("  no matches")))
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
	gap := m.listWidth() - lipgloss.Width(input) - lipgloss.Width(countStyled) - 1
	if gap < 0 {
		gap = 0
	}
	return lipgloss.NewStyle().Width(m.listWidth()).Render(input + strings.Repeat(" ", gap) + countStyled)
}

func (m Model) renderBranchItem(name string, selected, current bool) string {
	prefix := "  "
	if current {
		prefix = m.styles.StagedIcon.Render("* ")
	}
	line := prefix + truncatePath(name, m.listWidth()-4)
	if selected {
		return m.styles.FileSelected.Width(m.listWidth()).Render(line)
	}
	return m.styles.FileItem.Width(m.listWidth()).Render(line)
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

// truncatePath shortens a path from the front, which keeps the end that
// identifies it, and marks the cut with an ellipsis.
//
// It drops whole runes. Dropping bytes measured the same and rendered as
// mojibake — truncatePath("žluťoučký.ts", 4) came back as an ellipsis followed
// by half a rune — and the file list reaches it far more often now that names
// carry directories.
func truncatePath(path string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(path) <= maxW {
		return path
	}
	const ellipsis = "…"
	if maxW <= lipgloss.Width(ellipsis) {
		return ellipsis
	}
	runes := []rune(path)
	for len(runes) > 0 && lipgloss.Width(string(runes))+lipgloss.Width(ellipsis) > maxW {
		runes = runes[1:]
	}
	return ellipsis + string(runes)
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

// renderCommentEditor shows the textarea plus what the two closing keys do,
// inside budget rows.
func (m Model) renderCommentEditor(budget int) string {
	label := fmt.Sprintf(" comment · line %d ", m.draft.StartLine)
	if m.draft.EndLine > m.draft.StartLine {
		label = fmt.Sprintf(" comment · lines %d-%d ", m.draft.StartLine, m.draft.EndLine)
	}
	if m.editingID != "" {
		label = " edit" + label
	}
	head := m.renderBar(lipgloss.NewStyle(), m.styles.HelpKey.Render(label)+m.styles.HelpDesc.Render("· ctrl+s save · esc cancel"))

	// The textarea takes whatever is left rather than a fixed five rows. It was
	// fixed, and the frame was built as though the footer could always have it:
	// at height 8 the content area came out at -1 and View() panicked in
	// make([]string, -1). The head stays whatever happens — it is the only
	// place "ctrl+s save · esc cancel" is written.
	rows := max(budget-lipgloss.Height(head), 1)
	if rows < commentEditorHeight {
		m.commentInput.SetHeight(rows)
	}
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
