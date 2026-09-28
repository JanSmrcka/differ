package review

import (
	"fmt"
	"strings"
)

// Keeping comments pointing at the code they were written about.
//
// The loop this exists for: the reviewer comments, the agent edits the file,
// line numbers move. A comment is anchored by the *text* of the line it was
// written against, so it can follow that line when the diff shifts — and be
// marked stale when the line is gone, rather than quietly pointing at whatever
// now occupies its old number.

// Location is where one line of the current diff lives.
type Location struct {
	Side    Side
	Line    int
	Content string
}

// Reanchor re-resolves one file's comments against the current diff.
//
// A comment whose anchor text is still present on the same side follows it,
// keeping its span. One whose anchor has gone is marked stale, remembering the
// state it had so it can be restored if the anchor comes back — a file saved
// mid-edit should not permanently stale a review.
//
// Where the anchor text appears several times (a lone "}" is common) the
// nearest occurrence within maxAnchorDrift lines wins, and no two comments may
// land on the same line. Beyond that distance the match is treated as a
// different block and the comment goes stale: identical text is not evidence
// that it is the same code.
func (s *Session) Reanchor(file string, current []Location) {
	claimed := map[int]bool{}
	for i := range s.comments {
		c := &s.comments[i]
		if c.File != file || c.Anchor == "" {
			continue
		}

		line, ok := nearest(current, c.Side, c.Anchor, c.StartLine, claimed)
		switch {
		case !ok:
			s.markStale(c, fmt.Sprintf("line no longer in the diff: %q", shorten(c.Anchor, 44)))
			continue
		case abs(line-c.StartLine) > maxAnchorDrift:
			s.markStale(c, fmt.Sprintf("line moved too far to be sure it is the same code: %q", shorten(c.Anchor, 44)))
			continue
		}

		claimed[line] = true
		if delta := line - c.StartLine; delta != 0 {
			c.StartLine += delta
			c.EndLine += delta
		}
		s.restore(c)
	}
}

// maxAnchorDrift is how far a comment may follow its text before the match
// stops being believable. Ordinary edits move lines by a handful; a match
// hundreds of lines away is a different block that happens to read the same.
const maxAnchorDrift = 40

// StaleMissingFiles marks comments whose file is no longer part of the diff.
func (s *Session) StaleMissingFiles(files []string) {
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f] = true
	}
	for i := range s.comments {
		c := &s.comments[i]
		if present[c.File] {
			continue
		}
		s.markStale(c, "the file is no longer part of the diff")
	}
}

// markStale records why a comment no longer matches, remembering the state it
// is leaving so a later Reanchor can put it back — and so a delivered comment
// still reports WasSent and cannot be sent twice.
func (s *Session) markStale(c *Comment, reason string) {
	if c.State == StateStale {
		return
	}
	c.stateBeforeStale = c.State
	c.State = StateStale
	c.StaleReason = reason
}

// restore undoes markStale once a comment matches again.
func (s *Session) restore(c *Comment) {
	if c.State != StateStale {
		return
	}
	c.State = c.stateBeforeStale
	c.StaleReason = ""
}

// nearest finds the occurrence of anchor on the given side closest to want,
// ignoring lines another comment has already claimed.
func nearest(current []Location, side Side, anchor string, want int, claimed map[int]bool) (int, bool) {
	best, found := 0, false
	for _, l := range current {
		if l.Side != side || l.Content != anchor || claimed[l.Line] {
			continue
		}
		if !found || abs(l.Line-want) < abs(best-want) {
			best, found = l.Line, true
		}
	}
	return best, found
}

// shorten trims text for a one-line reason that has to fit a diff panel.
// It counts runes, not bytes: slicing bytes would cut a multibyte character in
// half and emit invalid UTF-8.
func shorten(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) <= max || max < 1 {
		return trimmed
	}
	return string(runes[:max-1]) + "…"
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
