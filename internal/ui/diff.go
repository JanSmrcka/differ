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
	LineContext DiffLineType = iota
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

// Hunk is one @@ section of a diff, with its position in ParsedDiff.Lines.
// Review comments anchor to hunks, so the line range must stay exact.
type Hunk struct {
	Index    int
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	// Context is the text git puts after the closing @@, usually the
	// enclosing function signature.
	Context string
	// StartLine indexes this hunk's first entry in ParsedDiff.Lines — the @@
	// header where the diff has one, otherwise the first content line (a new
	// file is rendered without a header). LastLine indexes the final line
	// belonging to this hunk, inclusive.
	StartLine int
	LastLine  int
}

// ParsedDiff is the result of parsing a raw unified diff.
type ParsedDiff struct {
	Lines  []DiffLine
	Hunks  []Hunk
	Binary bool
}

const maxDiffLines = 10000

// ParseDiff parses raw unified diff output into structured lines.
func ParseDiff(raw string) ParsedDiff {
	if strings.Contains(raw, "Binary files") && strings.Contains(raw, "differ") {
		return ParsedDiff{Binary: true}
	}

	var lines []DiffLine
	var hunks []Hunk
	oldNum, newNum := 0, 0
	truncated := false

	for _, line := range strings.Split(raw, "\n") {
		if len(lines) >= maxDiffLines {
			lines = append(lines, DiffLine{
				Type: LineHunkHeader, Content: fmt.Sprintf("… truncated (%d+ lines)", maxDiffLines),
				OldNum: -1, NewNum: -1,
			})
			truncated = true
			break
		}
		if strings.HasPrefix(line, "@@") {
			r := parseHunkRanges(line)
			oldNum, newNum = r.oldStart, r.newStart
			hunks = append(hunks, Hunk{
				Index:     len(hunks),
				OldStart:  r.oldStart,
				OldCount:  r.oldCount,
				NewStart:  r.newStart,
				NewCount:  r.newCount,
				Context:   extractHunkContext(line),
				StartLine: len(lines),
			})
			lines = append(lines, DiffLine{Type: LineHunkHeader, Content: extractHunkContext(line), OldNum: -1, NewNum: -1})
			continue
		}
		dl := parseDiffLine(line, &oldNum, &newNum)
		if dl != nil {
			lines = append(lines, *dl)
		}
	}

	// The truncation marker is appended as a header line but is not a hunk.
	end := len(lines) - 1
	if truncated {
		end = len(lines) - 2
	}
	for i := range hunks {
		if i+1 < len(hunks) {
			hunks[i].LastLine = hunks[i+1].StartLine - 1
		} else {
			hunks[i].LastLine = end
		}
		if hunks[i].LastLine < hunks[i].StartLine {
			hunks[i].LastLine = hunks[i].StartLine
		}
	}
	return ParsedDiff{Lines: lines, Hunks: hunks}
}

// hunkRanges holds the four numbers in an @@ -a,b +c,d @@ header.
type hunkRanges struct {
	oldStart, oldCount, newStart, newCount int
}

// parseHunkRanges reads both ranges from a hunk header. A missing count means
// 1, per the unified diff format.
func parseHunkRanges(line string) hunkRanges {
	r := hunkRanges{oldCount: 1, newCount: 1}
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) < 2 {
		return r
	}
	for _, field := range strings.Fields(strings.TrimSpace(parts[1])) {
		if len(field) < 2 {
			continue
		}
		start, count := parseRange(field[1:])
		switch field[0] {
		case '-':
			r.oldStart, r.oldCount = start, count
		case '+':
			r.newStart, r.newCount = start, count
		}
	}
	return r
}

func parseRange(s string) (start, count int) {
	count = 1
	nums := strings.SplitN(s, ",", 2)
	if n, err := strconv.Atoi(nums[0]); err == nil {
		start = n
	}
	if len(nums) == 2 {
		if n, err := strconv.Atoi(nums[1]); err == nil {
			count = n
		}
	}
	return start, count
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

const lineNumWidth = 4

// cursorMarker flags the current line. The gutter it lives in is always
// reserved, so lines do not shift horizontally as the cursor moves.
const cursorMarker = "▌"

const gutterWidth = 2

// defaultTabWidth is used where no configured width is available (the log
// browser), matching config.Default().
const defaultTabWidth = 4

// expandTabs replaces tabs with spaces to the next tab stop. A tab measures as
// one column but the terminal draws it as several, so leaving them in makes
// every width calculation wrong and the line overflows its panel.
func expandTabs(s string, tabWidth int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	if tabWidth <= 0 {
		tabWidth = defaultTabWidth
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// commentMarker flags a line carrying review comments; commentBar prefixes the
// comment body rows beneath it.
const commentMarker = "●"

const commentBar = "▏"

// staleMarker flags a comment, or a file, whose comments no longer match the
// diff they were written against.
const staleMarker = "!"

func renderDiffLineGutter(dl DiffLine, filename string, styles Styles, t theme.Theme, width int, gutter string) string {
	switch dl.Type {
	case LineHunkHeader:
		return renderHunkLine(dl, styles, width, gutter)
	default:
		return renderCodeLine(dl, filename, styles, t, width, gutter)
	}
}

func blankGutter() string { return strings.Repeat(" ", gutterWidth) }

func cursorGutter(styles Styles) string {
	return styles.Accent.Render(cursorMarker) + " "
}

func commentGutter(styles Styles) string {
	return styles.CommentBar.Render(commentMarker) + " "
}

func renderHunkLine(dl DiffLine, styles Styles, width int, gutter string) string {
	prefix := gutter + styles.DiffLineNum.Render("    ···  ")
	text := dl.Content
	if text != "" {
		text = " " + text
	}
	return prefix + styles.DiffHunkHeader.Render(text)
}

func renderCodeLine(dl DiffLine, filename string, styles Styles, t theme.Theme, width int, gutter string) string {
	oldNum := fmtLineNum(dl.OldNum)
	newNum := fmtLineNum(dl.NewNum)

	indicator := " "
	var bgColor string
	var numStyle lipgloss.Style
	var indStyle lipgloss.Style
	var bgStyle lipgloss.Style
	switch dl.Type {
	case LineAdded:
		indicator = "+"
		bgColor = t.AddedBg
		numStyle = styles.DiffLineNumAdded
		indStyle = styles.DiffAdded
		bgStyle = styles.DiffAddedBg
	case LineRemoved:
		indicator = "-"
		bgColor = t.RemovedBg
		numStyle = styles.DiffLineNumRemoved
		indStyle = styles.DiffRemoved
		bgStyle = styles.DiffRemovedBg
	default:
		numStyle = styles.DiffLineNum
		indStyle = styles.DiffContext
		bgStyle = lipgloss.NewStyle()
	}

	nums := numStyle.Render(oldNum + " " + newNum)

	// Syntax highlight the content
	highlighted := highlightLine(dl.Content, filename, bgColor)

	// Build: colored indicator + highlighted content + bg padding to fill width
	codeWidth := width - gutterWidth - lineNumWidth*2 - 3 // gutter + nums + spaces
	prefix := indStyle.Render(indicator + " ")
	contentWidth := lipgloss.Width(prefix) + lipgloss.Width(highlighted)
	padding := ""
	if pad := codeWidth - contentWidth; pad > 0 {
		padding = bgStyle.Render(strings.Repeat(" ", pad))
	}

	return gutter + nums + " " + prefix + highlighted + padding
}

func fmtLineNum(n int) string {
	if n < 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

// RenderBinaryFile renders a placeholder for binary files.
func RenderBinaryFile(styles Styles, width int) string {
	return styles.DiffHunkHeader.Width(width).Render("  Binary file — cannot display diff")
}

// --- Split (side-by-side) diff ---

const minSplitWidth = 60

// splitRow is a paired split line that remembers where each side came from in
// ParsedDiff.Lines, so a cursor keeps the same address in both views.
type splitRow struct {
	left, right       *DiffLine
	leftIdx, rightIdx int // index into the source lines, -1 when absent
}

// pairLinesIndexed pairs removed lines with the added lines that replace them,
// keeping each side's source index.
func pairLinesIndexed(lines []DiffLine) []splitRow {
	var rows []splitRow
	i := 0
	for i < len(lines) {
		switch lines[i].Type {
		case LineHunkHeader:
			rows = append(rows, splitRow{left: &lines[i], leftIdx: i, rightIdx: -1})
			i++
		case LineContext:
			rows = append(rows, splitRow{left: &lines[i], right: &lines[i], leftIdx: i, rightIdx: i})
			i++
		case LineRemoved:
			// Collect contiguous removed, then contiguous added.
			start := i
			for i < len(lines) && lines[i].Type == LineRemoved {
				i++
			}
			removedEnd := i
			for i < len(lines) && lines[i].Type == LineAdded {
				i++
			}
			addedEnd := i

			nRemoved := removedEnd - start
			nAdded := addedEnd - removedEnd
			for j := 0; j < max(nRemoved, nAdded); j++ {
				row := splitRow{leftIdx: -1, rightIdx: -1}
				if j < nRemoved {
					row.left, row.leftIdx = &lines[start+j], start+j
				}
				if j < nAdded {
					row.right, row.rightIdx = &lines[removedEnd+j], removedEnd+j
				}
				rows = append(rows, row)
			}
		case LineAdded:
			// Added with no preceding removed.
			rows = append(rows, splitRow{right: &lines[i], leftIdx: -1, rightIdx: i})
			i++
		default:
			i++
		}
	}
	return rows
}

const splitLineNumWidth = 4

func renderSplitSide(dl *DiffLine, filename string, styles Styles, t theme.Theme, panelW int, isLeft bool) string {
	if dl == nil {
		if panelW > 0 {
			return strings.Repeat(" ", panelW)
		}
		return ""
	}

	// Pick line number
	num := dl.OldNum
	if !isLeft {
		num = dl.NewNum
	}
	numStr := fmtLineNum(num)

	// Style selection
	indicator := " "
	var bgColor string
	var numStyle lipgloss.Style
	var indStyle lipgloss.Style
	var bgStyle lipgloss.Style

	switch dl.Type {
	case LineAdded:
		indicator = "+"
		bgColor = t.AddedBg
		numStyle = styles.DiffLineNumAdded
		indStyle = styles.DiffAdded
		bgStyle = styles.DiffAddedBg
	case LineRemoved:
		indicator = "-"
		bgColor = t.RemovedBg
		numStyle = styles.DiffLineNumRemoved
		indStyle = styles.DiffRemoved
		bgStyle = styles.DiffRemovedBg
	default:
		numStyle = styles.DiffLineNum
		indStyle = styles.DiffContext
		bgStyle = lipgloss.NewStyle()
	}

	nums := numStyle.Render(numStr)
	highlighted := highlightLine(dl.Content, filename, bgColor)
	prefix := indStyle.Render(indicator + " ")

	codeWidth := max(0, panelW-splitLineNumWidth-3)
	contentWidth := lipgloss.Width(prefix) + lipgloss.Width(highlighted)
	padding := ""
	if pad := codeWidth - contentWidth; pad > 0 {
		padding = bgStyle.Render(strings.Repeat(" ", pad))
	}

	return nums + " " + prefix + highlighted + padding
}
