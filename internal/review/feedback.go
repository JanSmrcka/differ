package review

import (
	"fmt"
	"sort"
	"strings"
)

// Turning review comments into a message a coding agent can act on.
//
// The output is plain text and fully determined by the comments: the same
// comments always produce byte-identical output, whichever order they arrive
// in and whichever view they were written from. Nothing here reads the
// repository — a comment already carries the excerpt it needs.

const feedbackSeparator = "---"

// FormatFeedback renders one or more comments as a single message.
func FormatFeedback(cs []Comment) string {
	if len(cs) == 0 {
		return ""
	}

	ordered := make([]Comment, len(cs))
	copy(ordered, cs)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].File != ordered[j].File {
			return ordered[i].File < ordered[j].File
		}
		if ordered[i].StartLine != ordered[j].StartLine {
			return ordered[i].StartLine < ordered[j].StartLine
		}
		// seq is creation order; IDs are strings, where "c10" < "c9".
		if ordered[i].seq != ordered[j].seq {
			return ordered[i].seq < ordered[j].seq
		}
		return ordered[i].ID < ordered[j].ID
	})

	var b strings.Builder
	b.WriteString(header(len(ordered)))
	for i, c := range ordered {
		if i > 0 {
			b.WriteString("\n" + feedbackSeparator + "\n\n")
		}
		b.WriteString(FormatComment(c))
	}
	return b.String()
}

func header(n int) string {
	if n == 1 {
		return "Review feedback: 1 comment\n\n"
	}
	return fmt.Sprintf("Review feedback: %d comments\n\n", n)
}

// FormatComment renders a single comment: where it is, what changed there,
// and what the reviewer said.
func FormatComment(c Comment) string {
	var b strings.Builder

	fmt.Fprintf(&b, "File: %s\n", c.File)
	fmt.Fprintf(&b, "%s (%s)\n", lineLabel(c), sideLabel(c.Side))

	if excerpt := strings.TrimRight(c.Excerpt, "\n"); excerpt != "" {
		b.WriteString("\nChanged code:\n")
		b.WriteString(excerpt)
		b.WriteString("\n")
	}

	b.WriteString("\nComment:\n")
	b.WriteString(strings.TrimRight(c.Body, "\n"))
	b.WriteString("\n")
	return b.String()
}

func lineLabel(c Comment) string {
	if c.EndLine > c.StartLine {
		return fmt.Sprintf("Lines: %d-%d", c.StartLine, c.EndLine)
	}
	return fmt.Sprintf("Line: %d", c.StartLine)
}

// sideLabel says which version of the file the line numbers refer to, so a
// comment on deleted code is not mistaken for one on the current file.
func sideLabel(s Side) string {
	if s == SideOld {
		return "old"
	}
	return "new"
}
