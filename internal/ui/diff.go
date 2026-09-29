package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"

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

// lineNumWidth is the minimum width of one line-number column. A file whose
// numbers do not fit gets wider columns — see geometry.
const lineNumWidth = 4

// geometry is the per-diff column arithmetic every row shares: how wide the
// line-number columns have to be for this particular file, and how much room
// the panel gives.
//
// It is one value rather than two parameters because the row renderers already
// take four, and every new piece of layout would otherwise add another int to
// five signatures.
type geometry struct {
	// numW is the width of each line-number column, the same on both sides so
	// the two stay aligned with each other.
	numW int
	// width is the space the row has: the whole diff panel in unified view,
	// one side of it in split.
	width int
	// split says how many line-number columns the row carries. Unified shows
	// the old and the new number side by side; each half of a split row shows
	// one. Without this the hunk header, which spans the whole width in both
	// views, indented its text to the unified block and sat five columns right
	// of the code it heads.
	split bool
}

// numbersWidth is the space the line-number block occupies: both columns and
// the space between them in unified, one column in split.
func (g geometry) numbersWidth() int {
	if g.split {
		return g.numW
	}
	return g.numW*2 + 1
}

// diffGeometry sizes the line-number columns to the largest number the diff
// actually mentions, never below lineNumWidth so narrow diffs do not shuffle
// as the user moves between files.
//
// It is sized from the diff rather than the file, so a hunk whose context
// crosses a power of ten (starting at 9998, reaching 10000) does widen the
// columns. That is the intended trade: reading the file to count its lines
// would cost a read per file per refresh.
func diffGeometry(p ParsedDiff, width int) geometry {
	highest := 0
	for _, l := range p.Lines {
		highest = max(highest, l.OldNum, l.NewNum)
	}
	return geometry{numW: max(lineNumWidth, digits(highest)), width: width}
}

func digits(n int) int {
	if n <= 0 {
		return 1
	}
	d := 0
	for n > 0 {
		d++
		n /= 10
	}
	return d
}

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

func renderDiffLineGutter(dl DiffLine, filename string, styles Styles, t theme.Theme, g geometry, gutter string, chroma *chroma.Style) string {
	switch dl.Type {
	case LineHunkHeader:
		return renderHunkLine(dl, styles, g, gutter)
	default:
		return renderCodeLine(dl, filename, styles, t, g, gutter, chroma)
	}
}

func blankGutter() string { return strings.Repeat(" ", gutterWidth) }

func cursorGutter(styles Styles) string {
	return styles.Accent.Render(cursorMarker) + " "
}

func commentGutter(styles Styles) string {
	return styles.CommentBar.Render(commentMarker) + " "
}

// renderHunkLine is the break between hunks as well as the header of the next
// one: the context text, then a rule out to the edge of the panel.
//
// Without the rule two hunks forty lines apart read as one continuous stretch
// of code — the "···" prefix alone was not a visible boundary.
func renderHunkLine(dl DiffLine, styles Styles, g geometry, gutter string) string {
	marker := hunkMarker(g)
	room := g.width - gutterWidth - lipgloss.Width(marker)

	// The rule has to survive, so the text is cut to leave room for it.
	text, cut := clipCode(dl.Content, max(room-hunkRuleWidth-1, 0))
	head := styles.DiffHunkHeader.Render(text)
	if cut {
		head += styles.DiffMark.Render(truncationMarker)
	}
	if text != "" {
		head += " "
	}
	rule := strings.Repeat(horizontalRule, max(room-lipgloss.Width(head), 0))

	return clipRow(gutter+styles.DiffLineNum.Render(marker)+head+styles.Chrome.Render(rule), g.width)
}

// clipRow is the last guard on a row's width: whatever the arithmetic above
// worked out, nothing leaves here wider than the panel.
//
// It earns its place at widths the column budget cannot satisfy at all — the
// line-number block alone is wider than a 10-column panel, so every renderer
// had a floor it silently exceeded. MaxWidth is used rather than a slice
// because the row is already styled, and cutting runes off a styled string
// drops the reset and bleeds colour down the screen.
func clipRow(row string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(row) <= width {
		return row
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(row)
}

// hunkRuleWidth is the shortest rule that still reads as a break rather than
// as punctuation.
const hunkRuleWidth = 8

// hunkMarker stands in for a hunk header's line numbers, right-aligned in the
// number block and padded so the header's text begins in the same column as
// code does. Unified and split share it, so the gutter reads the same in both.
func hunkMarker(g geometry) string {
	const dots = "···"
	pad := max(g.numbersWidth()-lipgloss.Width(dots), 0)
	// Three trailing columns: the space after the numbers, plus the two the
	// +/- indicator occupies on a code line.
	return strings.Repeat(" ", pad) + dots + "   "
}

// lineStyles is how a line of one type is drawn. Unified and split view used
// to build this twice from two copies of the same switch, which is how the two
// views drifted apart; they now share it, and so share their conventions.
type lineStyles struct {
	// indicator is the +/- in the column before the code.
	indicator string
	// bgColor is the line's background, which Chroma must not override — it
	// is what says added or removed, and is applied per token.
	bgColor string
	num     lipgloss.Style
	ind     lipgloss.Style
	// bg pads the rest of the row so the background reaches the edge.
	bg lipgloss.Style
	// mark draws what differ adds inside the code column: the stand-in for
	// trailing whitespace, and the sign that the line was cut.
	mark lipgloss.Style
	// emph paints the part of the line that differs from the line it is
	// paired with in split view.
	emph lipgloss.Style
}

func stylesFor(dl DiffLine, styles Styles, t theme.Theme) lineStyles {
	switch dl.Type {
	case LineAdded:
		return lineStyles{
			indicator: "+", bgColor: t.AddedBg,
			num: styles.DiffLineNumAdded, ind: styles.DiffAdded,
			bg: styles.DiffAddedBg, mark: styles.DiffMarkAdded,
			emph: styles.DiffAddedEmph,
		}
	case LineRemoved:
		return lineStyles{
			indicator: "-", bgColor: t.RemovedBg,
			num: styles.DiffLineNumRemoved, ind: styles.DiffRemoved,
			bg: styles.DiffRemovedBg, mark: styles.DiffMarkRemoved,
			emph: styles.DiffRemovedEmph,
		}
	default:
		return lineStyles{
			indicator: " ",
			num:       styles.DiffLineNum, ind: styles.DiffContext,
			bg: lipgloss.NewStyle(), mark: styles.DiffMark,
		}
	}
}

func renderCodeLine(dl DiffLine, filename string, styles Styles, t theme.Theme, g geometry, gutter string, chroma *chroma.Style) string {
	ls := stylesFor(dl, styles, t)
	nums := ls.num.Render(fmtLineNum(dl.OldNum, g.numW) + " " + fmtLineNum(dl.NewNum, g.numW))

	codeWidth := g.width - gutterWidth - g.numbersWidth() - 1 // gutter, numbers, one space
	prefix := ls.ind.Render(ls.indicator + " ")

	// Unified view has no pairing — a removed line and the added line
	// replacing it are separate rows — so there is nothing to compare against.
	code := renderCode(dl, filename, ls, codeWidth-lipgloss.Width(prefix), span{}, chroma)
	padding := ""
	if pad := codeWidth - lipgloss.Width(prefix) - lipgloss.Width(code); pad > 0 {
		padding = ls.bg.Render(strings.Repeat(" ", pad))
	}

	return clipRow(gutter+nums+" "+prefix+code+padding, g.width)
}

// renderCode is the code half of a row: cut to fit, syntax highlighted, with
// trailing whitespace made visible.
//
// The trailing whitespace is always split off before highlighting, whatever
// the line type. Chroma appends a synthetic newline to a trailing whitespace
// token, and highlightLine writes token values verbatim — so a context line
// ending in spaces came back as *two* rows. DisplayRows then disagreed with
// what Content produced, and every row index below it, which is what RowFor
// and the cursor are addressed by, was off by one.
//
// Whether the whitespace is *marked* is a separate decision: only on a line
// the change touched. A context line carries whatever the file already had,
// and marking those would flag the whole file rather than the change.
func renderCode(dl DiffLine, filename string, ls lineStyles, maxW int, changed span, chroma *chroma.Style) string {
	text, cut := clipCode(dl.Content, maxW)
	body, trailing := splitTrailing(text)

	// The span was measured against the whole line, so it is re-fitted to
	// whatever survived the cut — and widened off any grapheme boundary it
	// landed inside, which would otherwise change the line's width.
	out := highlightSpan(chroma, body, filename, ls.bgColor, ls.emph, changed.snap(body))
	if trailing != "" {
		switch dl.Type {
		case LineAdded, LineRemoved:
			out += ls.mark.Render(strings.Repeat(whitespaceMarker, len([]rune(trailing))))
		default:
			out += ls.bg.Render(trailing)
		}
	}
	if cut {
		out += ls.mark.Render(truncationMarker)
	}
	return out
}

// whitespaceMarker stands in for a trailing space, which is invisible in a
// diff and exactly the sort of thing a reviewer is expected to catch.
const whitespaceMarker = "·"

// splitTrailing separates a line's trailing whitespace from its body.
//
// Only spaces: DiffRenderer expands tabs before anything is rendered, so a tab
// never reaches here. Trimming them anyway would be worse than not — a tab
// measures zero columns, so a column count of the trailing run would draw no
// markers at all. The markers are counted in runes for the same reason.
func splitTrailing(s string) (body, trailing string) {
	body = strings.TrimRight(s, " ")
	return body, s[len(body):]
}

func fmtLineNum(n, w int) string {
	if n < 0 {
		return strings.Repeat(" ", max(w, 0))
	}
	return fmt.Sprintf("%*d", w, n)
}

// truncationMarker ends a line that was too long for the panel. A silent cut
// would be worse than wrapping: the reader has to know the line continues.
const truncationMarker = "›"

// clipCode cuts a code line to maxW display columns, leaving room for the
// marker, and reports whether anything was removed.
//
// The marker is not appended here: the caller adds it after highlighting, so
// Chroma never lexes it and it keeps its own colour rather than whatever the
// lexer makes of a stray chevron.
//
// The first version dropped one rune at a time and re-measured the whole
// prefix, which is O(n²) — 8.8 seconds for an 80,000-column line, and that
// runs inside Update when the cursor lands on one. lipgloss's MaxWidth does
// the same job in 341µs at 150,000 columns, and is grapheme-aware, so a ZWJ
// emoji sequence is still never split.
func clipCode(s string, maxW int) (string, bool) {
	if maxW <= 0 {
		return "", s != ""
	}
	if lipgloss.Width(s) <= maxW {
		return s, false
	}
	room := maxW - lipgloss.Width(truncationMarker)
	if room <= 0 {
		return "", true
	}
	return lipgloss.NewStyle().MaxWidth(room).Render(s), true
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

func renderSplitSide(dl *DiffLine, filename string, styles Styles, t theme.Theme, g geometry, isLeft bool, changed span, chroma *chroma.Style) string {
	if dl == nil {
		if g.width > 0 {
			return strings.Repeat(" ", g.width)
		}
		return ""
	}

	// The left side shows the old numbering, the right the new.
	num := dl.OldNum
	if !isLeft {
		num = dl.NewNum
	}

	ls := stylesFor(*dl, styles, t)
	nums := ls.num.Render(fmtLineNum(num, g.numW))
	prefix := ls.ind.Render(ls.indicator + " ")

	codeWidth := max(0, g.width-g.numbersWidth()-1)
	code := renderCode(*dl, filename, ls, codeWidth-lipgloss.Width(prefix), changed, chroma)
	padding := ""
	if pad := codeWidth - lipgloss.Width(prefix) - lipgloss.Width(code); pad > 0 {
		padding = ls.bg.Render(strings.Repeat(" ", pad))
	}

	return clipRow(nums+" "+prefix+code+padding, g.width)
}
