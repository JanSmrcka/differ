package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// One visual language: every panel header has the same shape as the rows
// under it — a label on the left, its meta against the right edge — and the
// frame says each thing once.

// The label and the first row are adjacent. The blank line under every panel
// label cost a row in both panels and bought separation the rule above
// already provides.
func TestLanguage_PanelHeaderIsOneRow(t *testing.T) {
	m := chromeModel(t, 120, 30)
	lines := strings.Split(stripANSI(m.View()), "\n")

	// Line 0 is the header, line 1 the rule, line 2 the panel labels, so the
	// first file must be on line 3.
	if !strings.Contains(lines[2], "Files") {
		t.Fatalf("line 2 should be the panel labels, got %q", lines[2])
	}
	if !strings.Contains(lines[3], "client.ts") {
		t.Errorf("the first file should sit directly under the label, got %q", lines[3])
	}
}

// The panel header carries its own panel's summary, against the right edge,
// exactly where every row in that panel carries its own.
func TestLanguage_LeftPanelHeaderCountsTheChangeset(t *testing.T) {
	m := chromeModel(t, 120, 30)
	// Split on the divider, not at listWidth: the focus mark is multi-byte,
	// so a byte slice at a column number cuts the row short.
	label := strings.Split(stripANSI(m.View()), "\n")[2]
	left, _, ok := strings.Cut(label, verticalDivider)
	if !ok {
		t.Fatalf("no divider in the label row: %q", label)
	}

	if !strings.Contains(left, "Files") {
		t.Errorf("left panel should be labelled: %q", left)
	}
	for _, want := range []string{"4", "1 staged"} {
		if !strings.Contains(left, want) {
			t.Errorf("left panel header should carry %q: %q", want, left)
		}
	}
	if strings.Contains(left, "CHANGED FILES") {
		t.Errorf("the label should not shout: %q", left)
	}
}

// Said once. The counts belong to the file list, so the header bar carries
// identity and branch and stops there.
func TestLanguage_HeaderDoesNotRepeatTheFileCounts(t *testing.T) {
	m := chromeModel(t, 120, 30)
	header := strings.Split(stripANSI(m.View()), "\n")[0]

	if !strings.Contains(header, "differ") || !strings.Contains(header, "master") {
		t.Errorf("header should still name the tool and branch: %q", header)
	}
	if strings.Contains(header, "4 files") {
		t.Errorf("header repeats the file list's own summary: %q", header)
	}
}

// The diff panel header has the same shape: the file on the left, what is
// true of it against the right edge.
func TestLanguage_DiffPanelHeaderPutsStateOnTheRight(t *testing.T) {
	m := chromeModel(t, 120, 30)
	label := stripANSI(strings.Split(m.View(), "\n")[2])
	_, right, ok := strings.Cut(label, verticalDivider)
	if !ok {
		t.Fatalf("no divider in the label row: %q", label)
	}
	right = strings.TrimRight(right, " ")

	if !strings.Contains(right, "client.ts") {
		t.Fatalf("diff panel should name the file: %q", right)
	}
	if !strings.HasSuffix(right, "staged") {
		t.Errorf("the file's state should sit against the right edge: %q", right)
	}
	// Pushed to the edge, not merely written after the name.
	if !strings.Contains(right, "   ") {
		t.Errorf("the state is not right-aligned, just appended: %q", right)
	}
}

// The log browser's rows are columns, not words separated by two spaces.
func TestLanguage_LogRowsAlignTheDate(t *testing.T) {
	lm := logModelOn(t, 120, 32)
	lines := strings.Split(stripANSI(lm.View()), "\n")

	var rows []string
	for _, l := range lines {
		if strings.Contains(l, "ago") {
			rows = append(rows, strings.TrimRight(l, " "))
		}
	}
	if len(rows) < 2 {
		t.Fatalf("expected two commit rows, got %d:\n%s", len(rows), lm.View())
	}
	if w0, w1 := lipgloss.Width(rows[0]), lipgloss.Width(rows[1]); w0 != w1 {
		t.Errorf("commit rows do not end at the same column: %d vs %d\n%q\n%q", w0, w1, rows[0], rows[1])
	}
}

// The claim the panel header makes is that it has the same shape as the rows
// under it. That is only true if its right-hand column ends in the same
// column theirs does.
func TestLanguage_PanelHeaderMetaEndsWhereTheRowsDo(t *testing.T) {
	m := chromeModel(t, 120, 30)
	lines := strings.Split(stripANSI(m.View()), "\n")

	edge := func(row string) int {
		left, _, ok := strings.Cut(row, verticalDivider)
		if !ok {
			t.Fatalf("no divider in %q", row)
		}
		return lipgloss.Width(strings.TrimRight(left, " "))
	}

	header := edge(lines[2])          // Files … 4 · 1 staged
	firstRow := edge(lines[3])        // ● M client.ts … +1 -1
	if header != firstRow {
		t.Errorf("panel header's right column ends at %d, the rows' at %d:\n%q\n%q",
			header, firstRow, lines[2], lines[3])
	}
}

// One separator. The header used to join its parts with two spaces in one
// place and a middle dot in another, on the same line.
func TestLanguage_HeadersUseOneSeparator(t *testing.T) {
	m := chromeModel(t, 120, 30)
	m.mode = modeReview
	header := strings.TrimRight(strings.Split(stripANSI(m.View()), "\n")[0], " ")
	if strings.Contains(header, "  ") {
		t.Errorf("main header still pads with runs of spaces: %q", header)
	}

	logHeader := strings.Split(stripANSI(logModelOn(t, 120, 32).View()), "\n")[0]
	left, _, _ := strings.Cut(strings.TrimRight(logHeader, " "), "  ")
	if !strings.Contains(left, headerSep) {
		t.Errorf("log header does not use the shared separator: %q", left)
	}
}
