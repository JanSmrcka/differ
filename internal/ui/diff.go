package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DiffLineType classifies a line in a unified diff.
type DiffLineType int

const (
	LineContext    DiffLineType = iota
	LineAdded
	LineRemoved
	LineHunkHeader
	LineFileHeader
)

// DiffLine is a single parsed line from a unified diff.
type DiffLine struct {
	Type    DiffLineType
	Content string
	OldNum  int // -1 if N/A
	NewNum  int // -1 if N/A
}

// ParsedDiff is the result of parsing a raw unified diff.
type ParsedDiff struct {
	Lines  []DiffLine
	Binary bool
}

// ParseDiff parses raw unified diff output into structured lines.
func ParseDiff(raw string) ParsedDiff {
	if strings.Contains(raw, "Binary files") && strings.Contains(raw, "differ") {
		return ParsedDiff{Binary: true}
	}

	var lines []DiffLine
	oldNum, newNum := 0, 0

	for _, line := range strings.Split(raw, "\n") {
		dl := parseDiffLine(line, &oldNum, &newNum)
		if dl != nil {
			lines = append(lines, *dl)
		}
	}
	return ParsedDiff{Lines: lines}
}

func parseDiffLine(line string, oldNum, newNum *int) *DiffLine {
	switch {
	case strings.HasPrefix(line, "diff --git"):
		return &DiffLine{Type: LineFileHeader, Content: line, OldNum: -1, NewNum: -1}
	case strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"),
		strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "similarity"),
		strings.HasPrefix(line, "rename"),
		strings.HasPrefix(line, "old mode"),
		strings.HasPrefix(line, "new mode"):
		return &DiffLine{Type: LineFileHeader, Content: line, OldNum: -1, NewNum: -1}
	case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return &DiffLine{Type: LineFileHeader, Content: line, OldNum: -1, NewNum: -1}
	case strings.HasPrefix(line, "@@"):
		parseHunkHeader(line, oldNum, newNum)
		return &DiffLine{Type: LineHunkHeader, Content: line, OldNum: -1, NewNum: -1}
	case strings.HasPrefix(line, "+"):
		dl := &DiffLine{Type: LineAdded, Content: line[1:], OldNum: -1, NewNum: *newNum}
		*newNum++
		return dl
	case strings.HasPrefix(line, "-"):
		dl := &DiffLine{Type: LineRemoved, Content: line[1:], OldNum: *oldNum, NewNum: -1}
		*oldNum++
		return dl
	case strings.HasPrefix(line, `\`):
		return nil // "\ No newline at end of file"
	case line == "":
		return nil
	default:
		// Context line (starts with space or no prefix)
		content := line
		if strings.HasPrefix(line, " ") {
			content = line[1:]
		}
		dl := &DiffLine{Type: LineContext, Content: content, OldNum: *oldNum, NewNum: *newNum}
		*oldNum++
		*newNum++
		return dl
	}
}

// parseHunkHeader extracts line numbers from @@ -old,count +new,count @@
func parseHunkHeader(line string, oldNum, newNum *int) {
	// Find the ranges between @@ markers
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) < 2 {
		return
	}
	ranges := strings.TrimSpace(parts[1])
	for _, r := range strings.Fields(ranges) {
		if strings.HasPrefix(r, "-") {
			nums := strings.SplitN(r[1:], ",", 2)
			if n, err := strconv.Atoi(nums[0]); err == nil {
				*oldNum = n
			}
		} else if strings.HasPrefix(r, "+") {
			nums := strings.SplitN(r[1:], ",", 2)
			if n, err := strconv.Atoi(nums[0]); err == nil {
				*newNum = n
			}
		}
	}
}

// RenderDiff renders parsed diff lines into a styled string.
func RenderDiff(parsed ParsedDiff, styles Styles, width int) string {
	if parsed.Binary {
		return RenderBinaryFile(styles, width)
	}

	var b strings.Builder
	for _, dl := range parsed.Lines {
		b.WriteString(renderDiffLine(dl, styles, width))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderDiffLine(dl DiffLine, styles Styles, width int) string {
	switch dl.Type {
	case LineHunkHeader:
		return styles.DiffHunkHeader.Width(width).Render(dl.Content)
	case LineFileHeader:
		return styles.DiffFileHeader.Render(dl.Content)
	default:
		return renderCodeLine(dl, styles, width)
	}
}

func renderCodeLine(dl DiffLine, styles Styles, width int) string {
	oldNum := fmtLineNum(dl.OldNum)
	newNum := fmtLineNum(dl.NewNum)
	nums := styles.DiffLineNum.Render(oldNum + " " + newNum)

	indicator := " "
	var lineStyle lipgloss.Style
	switch dl.Type {
	case LineAdded:
		indicator = "+"
		lineStyle = styles.DiffAdded
	case LineRemoved:
		indicator = "-"
		lineStyle = styles.DiffRemoved
	default:
		lineStyle = styles.DiffContext
	}

	content := lineStyle.Render(indicator + " " + dl.Content)
	return nums + " " + content
}

func fmtLineNum(n int) string {
	if n < 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

// RenderNewFile renders file content as an all-added diff (for untracked files).
func RenderNewFile(content string, styles Styles, width int) string {
	var b strings.Builder
	b.WriteString(styles.DiffFileHeader.Render("new file"))
	b.WriteByte('\n')

	for i, line := range strings.Split(content, "\n") {
		num := i + 1
		nums := styles.DiffLineNum.Render("     " + fmt.Sprintf("%4d", num))
		styled := styles.DiffAdded.Render("+ " + line)
		b.WriteString(nums + " " + styled)
		b.WriteByte('\n')
	}
	return b.String()
}

// RenderBinaryFile renders a placeholder for binary files.
func RenderBinaryFile(styles Styles, width int) string {
	return styles.DiffHunkHeader.Width(width).Render("Binary file — cannot display diff")
}
