package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

// branchModel opens the branch picker over a real changeset.
func branchModel(t *testing.T, w, h int, branches ...string) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: w, Height: h})

	if len(branches) == 0 {
		branches = []string{"master", "feat/one", "feat/two"}
	}
	loaded, _ := m.handleBranchesLoaded(branchesLoadedMsg{
		branches: branches, current: branches[0],
	})
	return loaded.(Model)
}

// The picker is a box over the view, like the comment editor and the agent
// picker. It used to replace the file list panel, so choosing a branch cost
// you sight of the changeset you were looking at.
func TestBranchModal_IsABoxOverTheView(t *testing.T) {
	t.Parallel()
	m := branchModel(t, 120, 30)
	view := m.View()

	if _, _, first, last := boxGeometry(t, view); first < 0 || last <= first {
		t.Fatalf("no box drawn:\n%s", view)
	}
	// The file list is still there, header and contents.
	for _, want := range []string{"CHANGED FILES", "src.ts"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view lost %q while the picker was open:\n%s", want, view)
		}
	}
	// And so is the diff.
	if !strings.Contains(view, "CHANGED") {
		t.Errorf("the diff is not visible behind the box:\n%s", view)
	}
	// The left panel no longer calls itself BRANCHES, because it is not the
	// branch list any more.
	if strings.Contains(view, "BRANCHES") {
		t.Errorf("the file list panel is still labelled BRANCHES:\n%s", view)
	}
}

// Everything the picker needs is inside the box: the branches, which one is
// current, and how many the filter matches.
func TestBranchModal_HoldsTheListAndTheCount(t *testing.T) {
	t.Parallel()
	m := branchModel(t, 120, 30)

	view := m.View()
	_, _, first, last := boxGeometry(t, view)
	rows := strings.Split(view, "\n")
	box := strings.Join(rows[first:last+1], "\n")

	for _, want := range []string{"branch", "master", "feat/one", "feat/two", "3/3"} {
		if !strings.Contains(box, want) {
			t.Errorf("the box does not carry %q:\n%s", want, box)
		}
	}

	// Which one is checked out, marked with a glyph rather than a colour:
	// #56's rule is that every distinction carries a mark or a word too.
	// Dropping the marker left the suite green.
	var marked string
	for _, row := range strings.Split(box, "\n") {
		if strings.Contains(row, "*") {
			marked = row
		}
	}
	if marked == "" {
		t.Errorf("nothing says which branch is checked out:\n%s", box)
	}
	if !strings.Contains(marked, "master") {
		t.Errorf("the marker is on %q, and master is the branch checked out", strings.TrimSpace(marked))
	}
}

// Filtering narrows the list inside the box and the count follows.
func TestBranchModal_FilteringHappensInTheBox(t *testing.T) {
	t.Parallel()
	m := branchModel(t, 120, 30)

	for _, r := range "two" {
		updated, _ := m.updateBranchMode(key(string(r)))
		m = updated.(Model)
	}

	view := m.View()
	_, _, first, last := boxGeometry(t, view)
	box := strings.Join(strings.Split(view, "\n")[first:last+1], "\n")

	if !strings.Contains(box, "feat/two") {
		t.Errorf("the match is not in the box:\n%s", box)
	}
	if strings.Contains(box, "feat/one") {
		t.Errorf("a branch the filter excludes is still shown:\n%s", box)
	}
	if !strings.Contains(box, "1/3") {
		t.Errorf("the count does not follow the filter:\n%s", box)
	}
}

// ctrl+n turns the same box into the new-branch prompt, rather than opening a
// bar at the bottom of the screen.
func TestBranchModal_CreatingABranchStaysInTheBox(t *testing.T) {
	t.Parallel()
	m := branchModel(t, 120, 30)

	updated, _ := m.updateBranchMode(key("ctrl+n"))
	m = updated.(Model)
	if !m.branchCreating {
		t.Fatal("ctrl+n did not start a new branch")
	}

	view := m.View()
	_, _, first, last := boxGeometry(t, view)
	if first < 0 {
		t.Fatalf("no box drawn while naming a branch:\n%s", view)
	}
	box := strings.Join(strings.Split(view, "\n")[first:last+1], "\n")
	if !strings.Contains(box, "new branch") {
		t.Errorf("the box does not say what it is asking:\n%s", box)
	}
	// The prompt, not just the title: asserting on the title alone passed
	// while the body still rendered the branch list, because the title is a
	// separate function.
	m.branchInput.SetValue("feat/typed-here")
	if body := strings.Join(m.branchRows(10), "\n"); !strings.Contains(body, "feat/typed-here") {
		t.Errorf("the box is not where the name is typed:\n%s", body)
	}
	// And the list is not also in there, which would be two questions at once.
	if body := strings.Join(m.branchRows(10), "\n"); strings.Contains(body, "feat/one") {
		t.Errorf("the branch list is still drawn while naming a branch:\n%s", body)
	}
	// The footer is not also asking.
	footer := strings.Join(strings.Split(view, "\n")[last+1:], "\n")
	if strings.Contains(footer, "new branch") {
		t.Errorf("the prompt is in the footer as well as the box:\n%s", footer)
	}
}

// A list longer than the box scrolls, and the highlighted branch is always on
// screen — the same arithmetic the agent picker needed.
func TestBranchModal_TheHighlightedBranchIsAlwaysOnScreen(t *testing.T) {
	t.Parallel()
	var many []string
	for i := range 40 {
		many = append(many, "branch-"+strconv.Itoa(i))
	}

	for _, h := range []int{14, 20, 24, 30, 40} {
		m := branchModel(t, 120, h, many...)
		for range len(many) - 1 {
			updated, _ := m.updateBranchMode(key("down"))
			m = updated.(Model)
		}

		view := m.View()
		want := many[m.branchCursor]
		if !strings.Contains(view, want) {
			t.Errorf("h=%d: the highlighted branch %q is not on screen:\n%s", h, want, view)
		}
	}
}

// The bar advertises the picker's keys while it is open, not the file list's.
func TestBranchModal_TheBarAdvertisesThePickersKeys(t *testing.T) {
	t.Parallel()
	m := branchModel(t, 120, 30)
	view := m.View()

	if !strings.Contains(view, "switch") {
		t.Errorf("the bar does not offer the picker's keys:\n%s", view)
	}
	if strings.Contains(view, "tab stage") {
		t.Errorf("the bar still offers the file list's keys:\n%s", view)
	}
}
