package ui

// What changed *within* a pair of lines.
//
// Split view pairs a removed line with the added line that replaced it, and
// said nothing about the difference between them — a one-character change and
// a rewritten line looked the same, so the reader compared two 80-column
// strings by eye. Emphasising the part that moved is what makes the view worth
// using over unified.

// changedRange is the span of runes that differs between two lines: the text
// after their common prefix and before their common suffix.
//
// It returns one start and two ends, because the two sides can differ in
// length — an insertion is an empty span on the old side. Identical lines give
// an empty span on both, and the caller then emphasises nothing, which is
// right: a line that did not change has nothing to point at.
//
// This is a character-level comparison, not a word-level one. It is what the
// eye needs for the common case — an argument, an operator, a name — and it
// cannot mis-split a token, because it never looks at tokens.
func changedRange(oldLine, newLine string) (start, endOld, endNew int) {
	o, n := []rune(oldLine), []rune(newLine)

	for start < len(o) && start < len(n) && o[start] == n[start] {
		start++
	}

	// The suffix scan stops at the prefix, so the two can never cross over.
	suffix := 0
	for suffix < len(o)-start && suffix < len(n)-start &&
		o[len(o)-1-suffix] == n[len(n)-1-suffix] {
		suffix++
	}

	return start, len(o) - suffix, len(n) - suffix
}

// span is a range of runes within a line, in the line's own coordinates.
//
// An empty or inverted span marks nothing, which is how "these two lines are
// identical" and "these two have nothing in common" are both expressed: in the
// first there is nothing to point at, and in the second pointing at the whole
// line says no more than the +/- already does.
type span struct{ from, to int }

func (s span) marks() bool { return s.to > s.from }

// clamp brings a span computed against the whole line into the coordinates of
// however much of it survived being cut to the panel width.
func (s span) clamp(runes int) span {
	return span{from: min(s.from, runes), to: min(s.to, runes)}
}

// piece is a run of text that is either inside the changed span or outside it.
type piece struct {
	text string
	emph bool
}

// split cuts a piece of text — a whole line, or one token of it — at the
// span's boundaries. offset is where this text starts within the line.
func (s span) split(text string, offset int) []piece {
	if !s.marks() {
		return []piece{{text: text}}
	}
	runes := []rune(text)
	end := offset + len(runes)
	if s.to <= offset || s.from >= end {
		return []piece{{text: text}}
	}

	// Boundaries translated into this text's own indexes.
	lo := max(s.from-offset, 0)
	hi := min(s.to-offset, len(runes))

	var out []piece
	if lo > 0 {
		out = append(out, piece{text: string(runes[:lo])})
	}
	out = append(out, piece{text: string(runes[lo:hi]), emph: true})
	if hi < len(runes) {
		out = append(out, piece{text: string(runes[hi:])})
	}
	return out
}
