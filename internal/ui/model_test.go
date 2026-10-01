package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

func TestBuildFileItems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		changes   []git.FileChange
		untracked []string
		wantLen   int
	}{
		{"empty", nil, nil, 0},
		{"changes_only", []git.FileChange{{Path: "a.go", Status: git.StatusModified}}, nil, 1},
		{"untracked_only", nil, []string{"b.go"}, 1},
		{"mixed", []git.FileChange{{Path: "a.go", Status: git.StatusModified}}, []string{"b.go"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildFileItems(nil, tt.changes, tt.untracked)
			if len(got) != tt.wantLen {
				t.Errorf("len=%d, want %d", len(got), tt.wantLen)
			}
			// Verify untracked items have the flag set
			for _, f := range got {
				if f.change.Status == git.StatusUntracked && !f.untracked {
					t.Error("untracked item should have untracked=true")
				}
			}
		})
	}
}

func TestTruncatePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		maxW int
		want string // empty means "just check length"
	}{
		{"short", "file.go", 20, "file.go"},
		{"exact", "file.go", 7, "file.go"},
		{"long", "very-long-filename-that-exceeds-limit.go", 10, ""},
		{"single_char", "x", 1, "x"},
		{"boundary", "abc", 3, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncatePath(tt.path, tt.maxW)
			if tt.want != "" && got != tt.want {
				t.Errorf("truncatePath(%q, %d) = %q, want %q", tt.path, tt.maxW, got, tt.want)
			}
			if tt.want == "" {
				// For truncated paths, just verify it starts with ellipsis
				if !strings.HasPrefix(got, "…") {
					t.Errorf("expected truncated path to start with …, got %q", got)
				}
			}
		})
	}
}

func TestFilesEqual_Equal(t *testing.T) {
	t.Parallel()
	a := []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}}
	b := []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}}
	if !filesEqual(a, b) {
		t.Error("expected equal")
	}
}

func TestFilesEqual_DiffLength(t *testing.T) {
	t.Parallel()
	a := []fileItem{{change: git.FileChange{Path: "a.go"}}}
	b := []fileItem{{change: git.FileChange{Path: "a.go"}}, {change: git.FileChange{Path: "b.go"}}}
	if filesEqual(a, b) {
		t.Error("different lengths should not be equal")
	}
}

func TestFilesEqual_DiffContent(t *testing.T) {
	t.Parallel()
	a := []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}}
	b := []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusAdded}}}
	if filesEqual(a, b) {
		t.Error("different status should not be equal")
	}
}

func TestFilesEqual_BothEmpty(t *testing.T) {
	t.Parallel()
	if !filesEqual(nil, nil) {
		t.Error("two nil slices should be equal")
	}
}

func TestFilesEqual_OneEmpty(t *testing.T) {
	t.Parallel()
	a := []fileItem{{change: git.FileChange{Path: "a.go"}}}
	if filesEqual(a, nil) {
		t.Error("non-empty vs nil should not be equal")
	}
}

func TestContentHeight(t *testing.T) {
	t.Parallel()
	m := Model{height: 30}
	// 30 less the chrome (header and two rules = 3) and a one-row footer.
	if got := m.contentHeight(); got != 26 {
		t.Errorf("contentHeight()=%d, want 26", got)
	}
}

// The two panels divide the terminal between them, with the diff taking what
// the file list does not. The file list's share is no longer fixed, so this
// checks the arithmetic holds at several widths rather than pinning one.
func TestDiffWidth(t *testing.T) {
	t.Parallel()
	for _, w := range []int{80, 100, 120, 160, 200} {
		m := Model{width: w, files: []fileItem{{}}}
		want := w - m.listWidth() - verticalDividerWidth - 2*panelGap
		if got := m.diffWidth(); got != want {
			t.Errorf("width %d: diffWidth() = %d, want %d", w, got, want)
		}
		// Nothing is unaccounted for: the two panels plus the divider and its
		// spaces are exactly the terminal.
		if total := m.listWidth() + verticalDividerWidth + 2*panelGap + m.diffWidth(); total != w {
			t.Errorf("width %d: the panels add up to %d", w, total)
		}
	}
}

func newTestModel(t *testing.T, files []fileItem) Model {
	t.Helper()
	th := theme.Themes["dark"]
	bf := textinput.New()
	bf.Placeholder = "filter..."
	bf.CharLimit = 100
	bf.Width = minListWidth - 8
	bi := textinput.New()
	bi.Placeholder = "branch name..."
	bi.CharLimit = 100
	return Model{
		files:        files,
		styles:       NewStyles(th),
		theme:        th,
		cfg:          config.Default(),
		width:        120,
		height:       30,
		commitInput:  textinput.New(),
		branchFilter: bf,
		branchInput:  bi,
	}
}

func TestRenderHeader_StagedCount(t *testing.T) {
	t.Parallel()
	files := []fileItem{
		{change: git.FileChange{Path: "a.go", Staged: true}},
		{change: git.FileChange{Path: "b.go", Staged: false}},
	}
	m := newTestModel(t, files)
	m.width = 100
	// Changeset counts live in the header, not repeated in the footer.
	header := m.renderHeader()
	if !strings.Contains(header, "1 staged") {
		t.Errorf("header should show staged count, got %q", header)
	}
	if !strings.Contains(header, "2 files") {
		t.Errorf("header should show file count, got %q", header)
	}
	if strings.Contains(m.statusSegment(), "staged") {
		t.Errorf("footer should not repeat the counts, got %q", m.statusSegment())
	}
}

func TestStatusSegment_SplitIndicator(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.splitDiff = true
	bar := m.statusSegment()
	if !strings.Contains(bar, "split") {
		t.Error("status bar should show split indicator when splitDiff=true")
	}
}

func TestStatusSegment_StatusMsg(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.statusMsg = "committed!"
	bar := m.statusSegment()
	if !strings.Contains(bar, "committed!") {
		t.Error("status bar should show status message")
	}
}

func TestRenderHelpBar_FileListMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	bar := m.renderHintBar()
	for _, key := range []string{"j/k", "enter", "tab", "q"} {
		if !strings.Contains(bar, key) {
			t.Errorf("file list help should contain %q", key)
		}
	}
}

func TestRenderHelpBar_DiffMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeDiff
	bar := m.renderHintBar()
	for _, key := range []string{"j/k", "esc", "n/p", "q"} {
		if !strings.Contains(bar, key) {
			t.Errorf("diff help should contain %q", key)
		}
	}
}

func TestRenderHelpBar_BranchMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	bar := m.renderHintBar()
	for _, key := range []string{"↑/^k", "↓/^j", "enter", "esc", "filter"} {
		if !strings.Contains(bar, key) {
			t.Errorf("branch help should contain %q", key)
		}
	}
}

func TestRenderHelpBar_FileListShowsBranches(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	bar := m.renderHintBar()
	if !strings.Contains(bar, "b") {
		t.Error("file list help should contain b for branches")
	}
}

func TestRenderFileItem_ShowsStats(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	item := fileItem{change: git.FileChange{Path: "main.go", Status: git.StatusModified, AddedLines: 12, DeletedLines: 3}}
	out := m.renderFileItem(item, false, nil)
	if !strings.Contains(out, "+12 -3") {
		t.Errorf("expected stats in file item, got %q", out)
	}
}

func TestUpdateBranchMode_Navigation(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev", "feature"}
	m.branchCursor = 0

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyDown})
	rm := result.(Model)
	if rm.branchCursor != 1 {
		t.Errorf("cursor=%d after down, want 1", rm.branchCursor)
	}

	result, _ = rm.updateBranchMode(tea.KeyMsg{Type: tea.KeyUp})
	rm = result.(Model)
	if rm.branchCursor != 0 {
		t.Errorf("cursor=%d after up, want 0", rm.branchCursor)
	}
}

func TestUpdateBranchMode_Esc(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main"}
	m.branchCursor = 0

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEscape})
	rm := result.(Model)
	if rm.mode != modeFileList {
		t.Errorf("mode=%d after esc, want modeFileList", rm.mode)
	}
}

// This reverses what the test used to assert. It required the tool's own words
// in the status bar; putting them there is the thing #55 set out to stop. The
// bar gets differ's sentence, and the original text is reachable with !.
func TestHandleBranchesLoaded_Error(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	msg := branchesLoadedMsg{err: fmt.Errorf("fatal: some git complaint")}
	result, _ := m.handleBranchesLoaded(msg)
	rm := result.(Model)
	if rm.mode != modeFileList {
		t.Error("should stay in file list mode on error")
	}
	if !strings.Contains(rm.statusMsg, "listing branches failed") {
		t.Errorf("statusMsg=%q, want it to say what failed", rm.statusMsg)
	}
	if rm.problem == nil || !strings.Contains(rm.problem.detail, "some git complaint") {
		t.Error("the original text was not kept for the details view")
	}
}

func TestHandleResize_ClearsDiffCache(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.go", Status: git.StatusModified}},
	})
	m.cursor = 0

	// Simulate having cached diff content
	m.lastDiffContent = "old diff"
	m.viewport.SetContent("old diff")

	// Resize creates new viewport — cache must be cleared
	result, _ := m.handleResize(tea.WindowSizeMsg{Width: 100, Height: 40})
	rm := result.(Model)

	if rm.lastDiffContent != "" {
		t.Error("handleResize should clear lastDiffContent to force re-apply")
	}

	// handleDiffLoaded with same content should apply (not skip) after resize
	result2, _ := rm.handleDiffLoaded(diffLoadedMsg{errContent: "old diff", index: 0})
	rm2 := result2.(Model)
	if rm2.lastDiffContent != "old diff" {
		t.Error("handleDiffLoaded should apply content after resize cleared cache")
	}
	if !strings.Contains(rm2.viewport.View(), "old diff") {
		t.Errorf("viewport should contain reapplied content, got %q", rm2.viewport.View())
	}
}

func TestHandleDiffLoaded_SkipsDuplicate(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.go", Status: git.StatusModified}},
	})
	m.cursor = 0
	m.lastDiffContent = "same diff"

	// Same content as cache — should be a no-op
	result, _ := m.handleDiffLoaded(diffLoadedMsg{errContent: "same diff", index: 0})
	rm := result.(Model)
	if rm.lastDiffContent != "same diff" {
		t.Error("cache should remain unchanged on duplicate")
	}
}

func TestFilterBranches(t *testing.T) {
	t.Parallel()
	branches := []string{"main", "feature-auth", "feature-ui", "bugfix-login", "dev"}

	t.Run("empty query returns nil", func(t *testing.T) {
		t.Parallel()
		if got := filterBranches(branches, ""); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("substring match", func(t *testing.T) {
		t.Parallel()
		got := filterBranches(branches, "feature")
		if len(got) != 2 {
			t.Fatalf("expected 2 matches, got %d: %v", len(got), got)
		}
	})
	t.Run("case insensitive", func(t *testing.T) {
		t.Parallel()
		got := filterBranches(branches, "FEATURE")
		if len(got) != 2 {
			t.Fatalf("expected 2 matches, got %d: %v", len(got), got)
		}
	})
	t.Run("no match", func(t *testing.T) {
		t.Parallel()
		got := filterBranches(branches, "zzz")
		if len(got) != 0 {
			t.Fatalf("expected 0 matches, got %d: %v", len(got), got)
		}
	})
}

func TestUpdateBranchMode_TypeFilters(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "feature-auth", "feature-ui", "dev"}
	m.branchFilter.Focus()

	// Type 'f' — should filter to feature branches
	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	rm := result.(Model)
	if rm.filteredBranches == nil {
		t.Fatal("filteredBranches should not be nil after typing")
	}
	if len(rm.filteredBranches) != 2 {
		t.Errorf("expected 2 filtered branches, got %d", len(rm.filteredBranches))
	}
	if rm.branchCursor != 0 {
		t.Errorf("cursor should reset to 0, got %d", rm.branchCursor)
	}
}

func TestUpdateBranchMode_EscClearsFilter(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "feature-auth", "dev"}
	m.branchFilter.Focus()
	m.branchFilter.SetValue("feat")
	m.filteredBranches = filterBranches(m.branches, "feat")

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEscape})
	rm := result.(Model)
	// First esc clears filter, stays in branch picker
	if rm.mode != modeBranchPicker {
		t.Errorf("mode=%d, want modeBranchPicker", rm.mode)
	}
	if rm.branchFilter.Value() != "" {
		t.Errorf("filter should be cleared, got %q", rm.branchFilter.Value())
	}
	if rm.filteredBranches != nil {
		t.Error("filteredBranches should be nil after clearing")
	}
}

func TestUpdateBranchMode_EscClosesWhenEmpty(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main"}
	m.branchFilter.Focus()
	// Filter is empty — esc should close
	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEscape})
	rm := result.(Model)
	if rm.mode != modeFileList {
		t.Errorf("mode=%d, want modeFileList", rm.mode)
	}
}

func TestUpdateBranchMode_ArrowsInFilteredList(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "feature-auth", "feature-ui", "dev"}
	m.filteredBranches = []string{"feature-auth", "feature-ui"}
	m.branchCursor = 0

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyDown})
	rm := result.(Model)
	if rm.branchCursor != 1 {
		t.Errorf("cursor=%d after down, want 1", rm.branchCursor)
	}
	// Should not go past end of filtered list
	result, _ = rm.updateBranchMode(tea.KeyMsg{Type: tea.KeyDown})
	rm = result.(Model)
	if rm.branchCursor != 1 {
		t.Errorf("cursor=%d, should not exceed filtered list", rm.branchCursor)
	}
}

func TestUpdateBranchMode_CtrlJK(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev", "feature"}
	m.branchCursor = 0

	// ctrl+j moves down
	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyCtrlJ})
	rm := result.(Model)
	if rm.branchCursor != 1 {
		t.Errorf("cursor=%d after ctrl+j, want 1", rm.branchCursor)
	}

	// ctrl+k moves up
	result, _ = rm.updateBranchMode(tea.KeyMsg{Type: tea.KeyCtrlK})
	rm = result.(Model)
	if rm.branchCursor != 0 {
		t.Errorf("cursor=%d after ctrl+k, want 0", rm.branchCursor)
	}
}

// The filter and the match count moved into the box with everything else.
func TestBranchRows_ShowTheFilterAndTheCount(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev"}
	m.branchCursor = 0

	out := strings.Join(m.branchRows(10), "\n")
	if !strings.Contains(out, "2/2") {
		t.Errorf("the box does not show the match count:\n%s", out)
	}
}

// A filter that matches nothing says so, rather than leaving an empty box
// that reads as a bug.
func TestBranchRows_SayWhenNothingMatches(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev"}
	m.filteredBranches = []string{}
	m.branchFilter.SetValue("zzz")

	out := strings.Join(m.branchRows(10), "\n")
	if !strings.Contains(out, "no matches") {
		t.Errorf("an empty result does not say so:\n%s", out)
	}
	if !strings.Contains(out, "0/2") {
		t.Errorf("the count does not say 0/2:\n%s", out)
	}
}

func TestUpdateBranchMode_CtrlN_EntersCreateMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev"}
	m.branchCursor = 0
	m.branchFilter.Focus()

	result, cmd := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyCtrlN})
	rm := result.(Model)
	if !rm.branchCreating {
		t.Error("ctrl+n should set branchCreating=true")
	}
	if rm.mode != modeBranchPicker {
		t.Error("should stay in branch picker mode")
	}
	if cmd == nil {
		t.Error("expected textinput.Blink cmd")
	}
}

func TestUpdateBranchMode_CreateMode_RoutesToInput(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()
	m.branches = []string{"main"}

	// Typing 'j' should go to text input, not move branch cursor
	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	rm := result.(Model)
	if rm.branchInput.Value() != "j" {
		t.Errorf("input=%q, want %q", rm.branchInput.Value(), "j")
	}
}

func TestUpdateBranchMode_CreateMode_Esc_Cancels(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()
	m.branchInput.SetValue("feature-x")
	m.branches = []string{"main"}

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEscape})
	rm := result.(Model)
	if rm.branchCreating {
		t.Error("esc should cancel branch creation")
	}
	if rm.branchInput.Value() != "" {
		t.Error("input should be reset on cancel")
	}
}

// This reverses what the test here used to assert. ctrl+c cancelled the
// new-branch input, while the keymap documented it as a global "quit
// immediately" — and in the commit input the same key was swallowed by the
// text field, so it meant three different things in three places. It quits
// everywhere now; esc is what cancels an input.
func TestUpdateBranchMode_CreateMode_EscCancels_CtrlCQuits(t *testing.T) {
	t.Parallel()
	base := func() Model {
		m := newTestModel(t, nil)
		m.mode = modeBranchPicker
		m.branchCreating = true
		m.branchInput.Focus()
		m.branches = []string{"main"}
		return m
	}

	result, _ := base().updateBranchMode(tea.KeyMsg{Type: tea.KeyEsc})
	if result.(Model).branchCreating {
		t.Error("esc should cancel branch creation")
	}

	_, cmd := base().Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did nothing in the new-branch input")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c should quit from the new-branch input")
	}
}

func TestUpdateBranchMode_CreateMode_Enter_EmptyName(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()
	m.branches = []string{"main"}

	result, cmd := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEnter})
	rm := result.(Model)
	if !strings.Contains(rm.statusMsg, "empty") {
		t.Errorf("statusMsg=%q, want empty branch name error", rm.statusMsg)
	}
	if cmd != nil {
		t.Error("should not issue cmd on empty name")
	}
}

func TestUpdateBranchMode_CreateMode_Enter_SubmitsCmd(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()
	m.branchInput.SetValue("feature-x")
	m.branches = []string{"main"}

	_, cmd := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("expected async create branch cmd")
	}
}

func TestHandleBranchCreated_Success(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true

	result, cmd := m.handleBranchCreated(branchCreatedMsg{name: "feature-x"})
	rm := result.(Model)
	if rm.mode != modeFileList {
		t.Errorf("mode=%d, want modeFileList", rm.mode)
	}
	if rm.branchCreating {
		t.Error("branchCreating should be false")
	}
	if !strings.Contains(rm.statusMsg, "feature-x") {
		t.Errorf("statusMsg=%q, want branch name", rm.statusMsg)
	}
	if cmd == nil {
		t.Error("expected refresh files cmd")
	}
}

func TestHandleBranchCreated_Error(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true

	result, cmd := m.handleBranchCreated(branchCreatedMsg{
		name: "bad",
		err:  fmt.Errorf("already exists"),
	})
	rm := result.(Model)
	if rm.mode != modeBranchPicker {
		t.Error("should stay in branch picker on error")
	}
	if !strings.Contains(rm.statusMsg, "already exists") {
		t.Errorf("statusMsg=%q, want error", rm.statusMsg)
	}
	if cmd != nil {
		t.Error("should not issue cmd on error")
	}
}

func TestRenderHelpBar_BranchMode_ShowsNewKey(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	bar := m.renderHintBar()
	if !strings.Contains(bar, "^n") {
		t.Error("branch help should contain ^n for new branch")
	}
	if !strings.Contains(bar, "new") {
		t.Error("branch help should contain 'new' description")
	}
}

func TestPush_NoUpstream_OffersSetUpstream(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	m.upstream = git.UpstreamInfo{} // no upstream
	m.currentBranch = "feature-x"

	// First P should offer set-upstream, not block
	result, _ := m.updateFileListMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	rm := result.(Model)
	if !strings.Contains(rm.statusMsg, "set-upstream") {
		t.Errorf("statusMsg=%q, should mention set-upstream", rm.statusMsg)
	}
	if !rm.pushConfirm {
		t.Error("should enter push confirm state")
	}
	if !strings.Contains(rm.statusMsg, "feature-x") {
		t.Errorf("statusMsg=%q, should mention branch name", rm.statusMsg)
	}
}

func TestPush_NoUpstream_ConfirmPushes(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	m.upstream = git.UpstreamInfo{} // no upstream
	m.pushConfirm = true
	m.currentBranch = "feature-x"

	// Second P should issue a push cmd
	result, cmd := m.updateFileListMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	rm := result.(Model)
	if rm.pushConfirm {
		t.Error("pushConfirm should be cleared")
	}
	if !strings.Contains(rm.statusMsg, "pushing") {
		t.Errorf("statusMsg=%q, should say pushing", rm.statusMsg)
	}
	if cmd == nil {
		t.Error("expected push cmd")
	}
}

func TestEnterCommitMode_NoStaged_SetsStatus(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Staged: false}}})

	result, cmd := m.enterCommitMode()
	rm := result.(Model)

	if cmd != nil {
		t.Error("expected no cmd")
	}
	if rm.mode != modeFileList {
		t.Errorf("mode=%v, want file list", rm.mode)
	}
	if !strings.Contains(rm.statusMsg, "no staged files") {
		t.Errorf("statusMsg=%q", rm.statusMsg)
	}
}

func TestEnterCommitMode_WithStaged_EntersCommitMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Staged: true}}})

	result, cmd := m.enterCommitMode()
	rm := result.(Model)

	if rm.mode != modeCommit {
		t.Errorf("mode=%v, want commit", rm.mode)
	}
	if !rm.generatingMsg {
		t.Error("generatingMsg should be true")
	}
	if cmd == nil {
		t.Error("expected batch cmd")
	}
}

func TestUpdateCommitMode_EnterEmpty_ShowsError(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeCommit

	result, cmd := m.updateCommitMode(tea.KeyMsg{Type: tea.KeyEnter})
	rm := result.(Model)

	if cmd != nil {
		t.Error("expected no cmd")
	}
	if !strings.Contains(rm.statusMsg, "empty commit message") {
		t.Errorf("statusMsg=%q", rm.statusMsg)
	}
}

func TestToggleStage_DisabledInStagedOnlyOrRef(t *testing.T) {
	t.Parallel()
	files := []fileItem{{change: git.FileChange{Path: "a.go", Staged: false}}}

	m1 := newTestModel(t, files)
	m1.stagedOnly = true
	_, cmd1 := m1.toggleStage()
	if cmd1 != nil {
		t.Error("toggleStage should be disabled for stagedOnly")
	}

	m2 := newTestModel(t, files)
	m2.ref = "main"
	_, cmd2 := m2.toggleStage()
	if cmd2 != nil {
		t.Error("toggleStage should be disabled for ref mode")
	}
}

func TestStageAll_DisabledInRefMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.ref = "main"

	_, cmd := m.stageAll()
	if cmd != nil {
		t.Error("stageAll should be disabled in ref mode")
	}
}

func TestHandleFilesRefreshed_EmptyClearsViewport(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go"}}})
	m.viewport.SetContent("old")

	result, cmd := m.handleFilesRefreshed(filesRefreshedMsg{files: nil})
	rm := result.(Model)

	if cmd != nil {
		t.Error("expected no cmd")
	}
	if rm.cursor != 0 {
		t.Errorf("cursor=%d, want 0", rm.cursor)
	}
	if rm.viewport.View() != "" {
		t.Errorf("viewport=%q, want empty", rm.viewport.View())
	}
}

func TestHandleTick_SkipsPollingDuringCommit(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeCommit

	_, cmd := m.handleTick()
	if cmd == nil {
		t.Fatal("expected tick cmd")
	}
	msg := cmd()
	if _, ok := msg.(tickMsg); !ok {
		t.Errorf("msg type=%T, want tickMsg", msg)
	}
}

func TestPushConfirm_ResetOnNonPKey(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	m.pushConfirm = true

	result, _ := m.updateFileListMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	rm := result.(Model)
	if rm.pushConfirm {
		t.Error("pushConfirm should reset on non-P key")
	}
}
