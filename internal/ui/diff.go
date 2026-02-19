package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
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

const maxDiffLines = 10000

// ParseDiff parses raw unified diff output into structured lines.
func ParseDiff(raw string) ParsedDiff {
	if strings.Contains(raw, "Binary files") && strings.Contains(raw, "differ") {
		return ParsedDiff{Binary: true}
	}

	var lines []DiffLine
	oldNum, newNum := 0, 0

	for _, line := range strings.Split(raw, "\n") {
		if len(lines) >= maxDiffLines {
			lines = append(lines, DiffLine{
				Type: LineHunkHeader, Content: fmt.Sprintf("… truncated (%d+ lines)", maxDiffLines),
				OldNum: -1, NewNum: -1,
			})
			break
		}
		dl := parseDiffLine(line, &oldNum, &newNum)
		if dl != nil {
			lines = append(lines, *dl)
		}
	}
	return ParsedDiff{Lines: lines}
}

func parseDiffLine(line string, oldNum, newNum *int) *DiffLine {
	switch {
	case strings.HasPrefix(line, "diff --git"),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"),
		strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "similarity"),
		strings.HasPrefix(line, "rename"),
		strings.HasPrefix(line, "old mode"),
		strings.HasPrefix(line, "new mode"),
		strings.HasPrefix(line, "--- "),
		strings.HasPrefix(line, "+++ "):
		// Skip raw git headers — we show a clean file banner instead
		return nil
	case strings.HasPrefix(line, "@@"):
		parseHunkHeader(line, oldNum, newNum)
		content := extractHunkContext(line)
		return &DiffLine{Type: LineHunkHeader, Content: content, OldNum: -1, NewNum: -1}
	case strings.HasPrefix(line, "+"):
		dl := &DiffLine{Type: LineAdded, Content: line[1:], OldNum: -1, NewNum: *newNum}
		*newNum++
		return dl
	case strings.HasPrefix(line, "-"):
		dl := &DiffLine{Type: LineRemoved, Content: line[1:], OldNum: *oldNum, NewNum: -1}
		*oldNum++
		return dl
	case strings.HasPrefix(line, `\`):
		return nil
	case line == "":
		return nil
	default:
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

// extractHunkContext pulls the function/context part from a hunk header.
// "@@ -13,6 +13,7 @@ func main() {" → "func main() {"
// "@@ -13,6 +13,7 @@" → ""
func extractHunkContext(line string) string {
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) == 3 {
		ctx := strings.TrimSpace(parts[2])
		if ctx != "" {
			return ctx
		}
	}
	// Show the range info as fallback
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return line
}

// parseHunkHeader extracts line numbers from @@ -old,count +new,count @@
func parseHunkHeader(line string, oldNum, newNum *int) {
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

const lineNumWidth = 4

// RenderDiff renders parsed diff lines into a styled string.
func RenderDiff(parsed ParsedDiff, filename string, styles Styles, t theme.Theme, width int) string {
	if parsed.Binary {
		return RenderBinaryFile(styles, width)
	}
	initChromaStyle(t.ChromaStyle)

	var b strings.Builder
	for _, dl := range parsed.Lines {
		b.WriteString(renderDiffLine(dl, filename, styles, t, width))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderDiffLine(dl DiffLine, filename string, styles Styles, t theme.Theme, width int) string {
	switch dl.Type {
	case LineHunkHeader:
		return renderHunkLine(dl, styles, width)
	default:
		return renderCodeLine(dl, filename, styles, t, width)
	}
}

func renderHunkLine(dl DiffLine, styles Styles, width int) string {
	prefix := styles.DiffLineNum.Render("    ···  ")
	text := dl.Content
	if text != "" {
		text = " " + text
	}
	return prefix + styles.DiffHunkHeader.Render(text)
}

func renderCodeLine(dl DiffLine, filename string, styles Styles, t theme.Theme, width int) string {
	oldNum := fmtLineNum(dl.OldNum)
	newNum := fmtLineNum(dl.NewNum)

	indicator := " "
	var bgColor string
	var numStyle lipgloss.Style
	switch dl.Type {
	case LineAdded:
		indicator = "+"
		bgColor = t.AddedBg
		numStyle = styles.DiffLineNumAdded
	case LineRemoved:
		indicator = "-"
		bgColor = t.RemovedBg
		numStyle = styles.DiffLineNumRemoved
	default:
		numStyle = styles.DiffLineNum
	}

	nums := numStyle.Render(oldNum + " " + newNum)

	// Syntax highlight the content
	highlighted := highlightLine(dl.Content, filename, bgColor)

	// Build the code portion with full-width background
	codeWidth := width - lineNumWidth*2 - 3 // nums + spaces
	var codeLine string
	switch dl.Type {
	case LineAdded:
		codeLine = styles.DiffAdded.Width(codeWidth).Render(indicator + " " + highlighted)
	case LineRemoved:
		codeLine = styles.DiffRemoved.Width(codeWidth).Render(indicator + " " + highlighted)
	default:
		codeLine = styles.DiffContext.Width(codeWidth).Render(indicator + " " + highlighted)
	}

	return nums + " " + codeLine
}

func fmtLineNum(n int) string {
	if n < 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

// RenderNewFile renders file content as an all-added diff (for untracked files).
func RenderNewFile(content, filename string, styles Styles, t theme.Theme, width int) string {
	initChromaStyle(t.ChromaStyle)

	var b strings.Builder
	codeWidth := width - lineNumWidth*2 - 3

	for i, line := range strings.Split(content, "\n") {
		num := i + 1
		nums := styles.DiffLineNumAdded.Render("     " + fmt.Sprintf("%4d", num))
		highlighted := highlightLine(line, filename, t.AddedBg)
		code := styles.DiffAdded.Width(codeWidth).Render("+ " + highlighted)
		b.WriteString(nums + " " + code)
		b.WriteByte('\n')
	}
	return b.String()
}

// RenderBinaryFile renders a placeholder for binary files.
func RenderBinaryFile(styles Styles, width int) string {
	return styles.DiffHunkHeader.Width(width).Render("  Binary file — cannot display diff")
}
