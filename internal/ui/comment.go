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
	staged := m.stagedOnly
	ref := m.ref
	untracked := map[string]bool{}
	for _, f := range m.files {
		if f.untracked {
			untracked[f.change.Path] = true
		}
	}

	return func() tea.Msg {
		out := make(map[string][]review.Location, len(targets))
		for _, path := range targets {
			parsed, ok := parseFileDiff(repo, path, staged, ref, untracked[path])
			if !ok {
				continue
			}
			out[path] = diffLocations(parsed)
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
	if m.renderer == nil {
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
		StartLine: line,
		EndLine:   line,
		HunkIndex: addr.HunkIndex,
		Anchor:    parsed.Lines[m.diffCursor].Content,
	}
	if h, ok := parsed.HunkAt(m.diffCursor); ok {
		c.Excerpt = excerptFor(parsed, h)
	}
	c.FileKey, c.Scope = m.contentKeyNow(c.File)
	return c, true
}

// contentKeyNow fingerprints the content the reviewer is looking at, right
// now, so a restored comment can be checked against the version its author
// read rather than against whatever the file held when the review was last
// written out.
//
// The scope is decided the way the diff was read, not by the flag differ was
// started with: git reports a file with both staged and unstaged changes
// twice, staged first, so in default mode the cursor can sit on an entry
// whose diff is `--cached`. Under -r the diff is a ref against the working
// tree whatever the index says.
func (m Model) contentKeyNow(path string) (string, review.KeyScope) {
	if m.repo == nil || path == "" {
		return "", review.ScopeWorktree
	}
	if m.readsTheIndex() {
		staged, err := m.repo.IndexHashes()
		return indexKey(staged, path, err == nil), review.ScopeIndex
	}
	return worktreeKey(m.repo, path), review.ScopeWorktree
}

// readsTheIndex reports whether the diff under the cursor came from the
// index rather than the working tree.
func (m Model) readsTheIndex() bool {
	if m.ref != "" {
		return false
	}
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return m.stagedOnly
	}
	return m.files[m.cursor].change.Staged
}

// buildHunkComment describes the whole hunk the cursor is in.
func (m Model) buildHunkComment() (review.Comment, bool) {
	if m.renderer == nil {
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

	c := review.Comment{
		File:      m.currentFilePath(),
		Side:      side,
		StartLine: start,
		EndLine:   start + max(count, 1) - 1,
		HunkIndex: h.Index,
		Anchor:    anchorForHunk(parsed, h, side),
		Excerpt:   excerptFor(parsed, h),
	}
	c.FileKey, c.Scope = m.contentKeyNow(c.File)
	return c, true
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
