package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

// cardLineWidths returns the rendered width of every line of a card.
func cardLineWidths(card string) []int {
	var out []int
	for _, l := range strings.Split(card, "\n") {
		out = append(out, lipgloss.Width(l))
	}
	return out
}

// A card is always w+2 wide: the content plus its two border columns.
// JoinVertical pads short lines, so an over-wide top border shows up as the
// whole card growing, which is what pushed the diff panel out of place.
func TestRenderCard_LongTitleDoesNotWidenTheCard(t *testing.T) {
	th := theme.Themes["dark"]
	card := renderCard(th, "docs/validate-user-guide-and-screenshots", "", true, fileListWidth, 3)

	if got, want := lipgloss.Width(card), fileListWidth+2; got != want {
		t.Errorf("card width = %d, want %d — a long title must not widen the card:\n%s", got, want, card)
	}
	for i, got := range cardLineWidths(card) {
		if got != fileListWidth+2 {
			t.Errorf("line %d has width %d, want %d", i, got, fileListWidth+2)
		}
	}
}

func TestRenderCard_LongTitleIsTruncatedNotDropped(t *testing.T) {
	th := theme.Themes["dark"]
	card := renderCard(th, "docs/validate-user-guide-and-screenshots", "", true, fileListWidth, 2)
	top := strings.Split(card, "\n")[0]

	// The start of the title must survive so the card stays identifiable.
	if !strings.Contains(top, "docs/") {
		t.Errorf("title was dropped entirely: %q", top)
	}
	if !strings.Contains(top, "…") {
		t.Errorf("a truncated title should be marked with an ellipsis: %q", top)
	}
	if !strings.HasSuffix(strings.TrimRight(top, " "), "╮") {
		t.Errorf("top border should still close with ╮: %q", top)
	}
}

func TestRenderCard_TitleExactlyFits(t *testing.T) {
	th := theme.Themes["dark"]
	// Longest title that fits: width - 3 leaves room for "╭─", two spaces, "╮".
	title := strings.Repeat("x", fileListWidth-4)
	card := renderCard(th, title, "", true, fileListWidth, 2)

	for i, got := range cardLineWidths(card) {
		if got != fileListWidth+2 {
			t.Errorf("line %d width %d, want %d for an exactly-fitting title", i, got, fileListWidth+2)
		}
	}
	if !strings.Contains(strings.Split(card, "\n")[0], title) {
		t.Error("a title that fits must not be truncated")
	}
}

func TestRenderCard_WidthsMatchAcrossTitleLengths(t *testing.T) {
	th := theme.Themes["dark"]
	for _, title := range []string{"", "a", "master", strings.Repeat("long-", 20)} {
		card := renderCard(th, title, "line", false, fileListWidth, 3)
		for i, got := range cardLineWidths(card) {
			if got != fileListWidth+2 {
				t.Errorf("title %q: line %d width %d, want %d", title, i, got, fileListWidth+2)
			}
		}
	}
}

// The layout symptom: a ragged left card pushes the diff panel out of place.
func TestView_LongBranchNameKeepsPanelsAligned(t *testing.T) {
	m := diffModel(t, "multi_hunk", 10)
	m.mode = modeFileList
	m.width, m.height = 120, 20

	long := renderCard(m.theme, "docs/validate-user-guide-and-screenshots", "", true, fileListWidth, 5)
	short := renderCard(m.theme, "master", "", true, fileListWidth, 5)

	if lipgloss.Width(long) != lipgloss.Width(short) {
		t.Errorf("card width depends on title length: %d vs %d", lipgloss.Width(long), lipgloss.Width(short))
	}
}

func TestDiffRenderer_InitialisesSyntaxHighlighting(t *testing.T) {
	chromaStyle = nil
	chromaStyleMu = sync.Once{}

	styles, th := testStyles()
	NewDiffRenderer(ParseNewFile("package ui\n"), "a.go", styles, th, 80)

	if chromaStyle == nil {
		t.Error("constructing a renderer must initialise the Chroma style, or every line renders unhighlighted")
	}
}

func TestRenderCodeLine_TabsDoNotOverflowThePanel(t *testing.T) {
	styles, th := testStyles()
	const width = 80

	// A tab renders as several columns in the terminal but measures as one,
	// so unexpanded tabs push the line past the panel and it wraps.
	parsed := ParseNewFile("func main() {\n\tif ok {\n\t\treturn 1\n\t}\n}\n")
	r := NewDiffRenderer(parsed, "a.go", styles, th, width)

	for i, line := range strings.Split(r.Content(-1), "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("line %d is %d columns, panel is %d: %q", i, got, width, line)
		}
		if strings.Contains(line, "\t") {
			t.Errorf("line %d still contains a raw tab: %q", i, line)
		}
	}
}

func TestExpandTabs(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"\tx", 4, "    x"},
		{"a\tb", 4, "a   b"},
		{"aaa\tb", 4, "aaa b"},
		{"aaaa\tb", 4, "aaaa    b"},
		{"\t\tx", 2, "    x"},
		{"no tabs", 4, "no tabs"},
		{"", 4, ""},
	}
	for _, tc := range tests {
		if got := expandTabs(tc.in, tc.width); got != tc.want {
			t.Errorf("expandTabs(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

func TestExpandTabs_RespectsConfiguredWidth(t *testing.T) {
	if got := expandTabs("\tx", 8); got != strings.Repeat(" ", 8)+"x" {
		t.Errorf("tab width 8 gave %q", got)
	}
}
