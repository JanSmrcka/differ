package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
)

// Building review comments from the diff cursor.
//
// A comment carries everything needed to explain itself later: the file, the
// lines on the side they belong to, and a plain-text excerpt of the change.
// Keeping the excerpt on the comment means feedback can be generated without
// re-reading the repository, and lets a comment outlive the diff it came from.

// maxExcerptChars caps the diff context stored with a comment. Whoever reads
// the feedback needs the change, not the file.
const maxExcerptChars = 1200

// diffLocations describes where every addressable line of a diff currently
// lives, which is what review.Reanchor matches comments against.
func diffLocations(p ParsedDiff) []review.Location {
	out := make([]review.Location, 0, len(p.Lines))
	for i := range p.Lines {
		addr, ok := p.AddressOf(i)
		if !ok || addr.Type == LineHunkHeader {
			continue
		}
		side, line := sideAndLine(addr)
		if line < 0 {
			continue
		}
		out = append(out, review.Location{Side: side, Line: line, Content: p.Lines[i].Content})
	}
	return out
}

// reanchorAllCmd re-resolves every commented file against its current diff.
//
// Only the file on screen is re-anchored when its diff loads, so without this
// a comment on any other file keeps line numbers that the agent has since
// moved — and would be delivered quoting the wrong place.
func (m Model) reanchorAllCmd() tea.Cmd {
	return m.reanchorCmd(false)
}

// reanchorCmd re-resolves commented files, optionally including the one on
// screen.
//
// The file on screen is normally re-anchored by handleDiffLoaded, so it is
// excluded here. While the diff is held that never runs — and re-anchoring is
// what marks a comment stale, so excluding it left a comment about a deleted
// line looking pending and sendable.
func (m Model) reanchorCmd(includeCurrent bool) tea.Cmd {
	if m.session == nil {
		return nil
	}
	var targets []string
	seen := map[string]bool{}
	if current := m.currentFilePath(); !includeCurrent {
		seen[current] = true
	}
	for _, c := range m.session.Comments() {
		if !seen[c.File] {
			seen[c.File] = true
			targets = append(targets, c.File)
		}
	}
	if len(targets) == 0 {
		return nil
	}

	repo := m.repo
	// The flag, not an entry: these are files the cursor is not on, so there
	// is no entry to ask. What matters is that the Locate below is derived
	// from the same answer, so the coordinates and the claim about them
	// cannot disagree.
	staged := m.stagedOnly
	ref := m.ref
	locate := review.LocateLine
	if staged && ref == "" {
		locate = review.LocateFile
	}
	untracked := map[string]bool{}
	for _, f := range m.files {
		if f.untracked {
			untracked[f.change.Path] = true
		}
	}

	return func() tea.Msg {
		out := make(map[string]review.Anchored, len(targets))
		for _, path := range targets {
			parsed, ok := parseFileDiff(repo, path, staged, ref, untracked[path])
			if !ok {
				continue
			}
			out[path] = review.Anchored{Locations: diffLocations(parsed), Locate: locate}
		}
		return reanchorMsg{locations: out}
	}
}

// parseFileDiff reads one file's current diff, returning false when it can no
// longer be read — a comment must not be staled on a transient failure.
func parseFileDiff(repo *git.Repo, path string, staged bool, ref string, untracked bool) (ParsedDiff, bool) {
	if untracked {
		raw, err := repo.ReadFileContent(path)
		if err != nil {
			return ParsedDiff{}, false
		}
		return ParseNewFile(raw), true
	}
	raw, err := repo.DiffFile(path, staged, ref)
	if err != nil {
		return ParsedDiff{}, false
	}
	return ParseDiff(raw), true
}

// buildLineComment describes the line under the cursor.
func (m Model) buildLineComment() (review.Comment, bool) {
	if !m.rendererIsTheCursorsFile() {
		return review.Comment{}, false
	}
	parsed := m.renderer.Parsed()
	addr, ok := parsed.AddressOf(m.diffCursor)
	if !ok || addr.Type == LineHunkHeader {
		return review.Comment{}, false
	}

	side, line := sideAndLine(addr)
	if line < 0 {
		return review.Comment{}, false
	}

	c := review.Comment{
		File:      m.currentFilePath(),
		Side:      side,
		Locate:    m.locateFor(side),
		StartLine: line,
		EndLine:   line,
		HunkIndex: addr.HunkIndex,
		Anchor:    parsed.Lines[m.diffCursor].Content,
	}
	if h, ok := parsed.HunkAt(m.diffCursor); ok {
		c.Excerpt = excerptFor(parsed, h)
	}
	return c, true
}

// buildHunkComment describes the whole hunk the cursor is in.
func (m Model) buildHunkComment() (review.Comment, bool) {
	if !m.rendererIsTheCursorsFile() {
		return review.Comment{}, false
	}
	parsed := m.renderer.Parsed()
	h, ok := parsed.HunkAt(m.diffCursor)
	if !ok {
		return review.Comment{}, false
	}

	start, count := h.NewStart, h.NewCount
	side := review.SideNew
	if count == 0 {
		// A hunk that only deletes has no new-side range.
		start, count, side = h.OldStart, h.OldCount, review.SideOld
	}

	return review.Comment{
		File:      m.currentFilePath(),
		Side:      side,
		Locate:    m.locateFor(side),
		StartLine: start,
		EndLine:   start + max(count, 1) - 1,
		HunkIndex: h.Index,
		Anchor:    anchorForHunk(parsed, h, side),
		Excerpt:   excerptFor(parsed, h),
	}, true
}

// rendererIsTheCursorsFile reports whether what is parsed is what the cursor
// is on.
//
// A comment takes its excerpt and anchor from the renderer and its path and
// Locate from the cursor. A diff load is a tea.Cmd and git diff costs about
// eight milliseconds, so holding j through the file list leaves the two
// disagreeing — and the comment then named one file while quoting another's
// hunk, or named the right file with the other entry's line numbers. Now
// that the reference is machine-actionable, that is a wrong edit rather than
// a confusing message. The editor already had this guard.
func (m Model) rendererIsTheCursorsFile() bool {
	return m.renderer != nil && m.rendererPath == m.currentFilePath()
}

// sideAndLine picks which side of the diff a line belongs to. A removed line
// exists only in the old file; everything else is addressed on the new one.
func sideAndLine(addr LineAddress) (review.Side, int) {
	if addr.Type == LineRemoved {
		return review.SideOld, addr.OldLine
	}
	return review.SideNew, addr.NewLine
}

// anchorForHunk is the hunk's first line that exists on the given side.
//
// The side matters: a hunk often starts with a removed line, which exists only
// on the old side. Anchoring a new-side comment to it would make the anchor
// unfindable and the comment stale against an unchanged diff.
func anchorForHunk(parsed ParsedDiff, h Hunk, side review.Side) string {
	for i := h.StartLine; i <= h.LastLine && i < len(parsed.Lines); i++ {
		dl := parsed.Lines[i]
		if dl.Type == LineHunkHeader {
			continue
		}
		if lineExistsOn(dl.Type, side) {
			return dl.Content
		}
	}
	return h.Context
}

// lineExistsOn reports whether a diff line is present in the given version of
// the file.
func lineExistsOn(t DiffLineType, side review.Side) bool {
	switch t {
	case LineAdded:
		return side == review.SideNew
	case LineRemoved:
		return side == review.SideOld
	default:
		return true // context lines exist on both sides
	}
}

// excerptFor renders a hunk as plain unified-diff text: no styling, no line
// numbers, just the change with its -/+ markers. Deterministic, so the same
// hunk always produces the same excerpt.
func excerptFor(parsed ParsedDiff, h Hunk) string {
	var b strings.Builder
	total := 0
	for i := h.StartLine; i <= h.LastLine && i < len(parsed.Lines); i++ {
		dl := parsed.Lines[i]
		if dl.Type == LineHunkHeader {
			continue
		}
		line := excerptMarker(dl.Type) + dl.Content
		if total+len(line)+1 > maxExcerptChars {
			fmt.Fprintf(&b, "… truncated (%d more lines)\n", h.LastLine-i+1)
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
		total += len(line) + 1
	}
	return b.String()
}

func excerptMarker(t DiffLineType) string {
	switch t {
	case LineAdded:
		return "+"
	case LineRemoved:
		return "-"
	default:
		return " "
	}
}

// locateFor says how precisely the agent can be pointed at a comment on this
// side of the current file.
//
// Only here is everything needed in scope: the side, whether the diff is
// against the index, and whether the file still exists. review cannot work it
// out — it deliberately reads nothing from the repository.
func (m Model) locateFor(side review.Side) review.Locate {
	if m.currentFileGone() {
		return review.LocateNone
	}
	// The old side describes the file before the change, so its line numbers
	// never resolve.
	if side == review.SideOld {
		return review.LocateFile
	}
	// Whether the *new* side is the working tree depends on what this entry's
	// diff was read with, not on the flag differ was started with. git reports
	// a file with both staged and unstaged changes twice, staged first — so in
	// default mode the cursor starts on an entry whose diff is `--cached`, and
	// keying on m.stagedOnly called that the worktree. Stage a change, edit
	// above it, and the reference said line 3 while the code had moved to line
	// 8. loadDiffCmd reads f.change.Staged; so does this.
	//
	// No `&& m.ref == ""` here. Under -r, changedFilesRef never sets Staged,
	// so the term could never be reached and removing it changed no screen —
	// a condition documenting a case it cannot see is worse than none.
	entry := m.files[m.cursor].change
	if entry.Staged && !m.worktreeMatchesIndex(entry.Path) {
		return review.LocateFile
	}
	return review.LocateLine
}

// currentFileGone reports whether there is no file on disk to point at: the
// cursor is off the end of the list, or the file under it has been deleted.
//
// The bounds check was one-sided. A negative cursor is not reachable today,
// but the only thing standing between it and a panic inside a tea.Cmd was
// that fact, and this function is the guard.
func (m Model) currentFileGone() bool {
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return true
	}
	return m.files[m.cursor].change.Status == git.StatusDeleted
}

// worktreeMatchesIndex answers whether a --cached diff's line numbers also
// address the file on disk.
//
// Blanket-degrading every staged entry to a file reference meant `differ
// commit` — whose whole purpose is reviewing staged work — could never point
// the agent at a line, even in its dominant case: stage, review, commit,
// with nothing edited in between, where the index and the working tree are
// the same bytes. The question is answerable exactly in one git call rather
// than guessed at conservatively.
//
// A failure answers false, which is the safe direction: an unanswerable
// question degrades to "@path", never to a line nobody checked.
func (m Model) worktreeMatchesIndex(path string) bool {
	if m.repo == nil {
		return false
	}
	same, err := m.repo.WorktreeMatchesIndex(path)
	return err == nil && same
}
