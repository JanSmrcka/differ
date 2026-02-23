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
	if got := m.contentHeight(); got != 27 {
		t.Errorf("contentHeight()=%d, want 27", got)
	}
}

func TestDiffWidth(t *testing.T) {
	t.Parallel()
	m := Model{width: 120}
	want := 120 - fileListWidth - 1
	if got := m.diffWidth(); got != want {
		t.Errorf("diffWidth()=%d, want %d", got, want)
	}
}

func newTestModel(t *testing.T, files []fileItem) Model {
	t.Helper()
	th := theme.Themes["dark"]
	bf := textinput.New()
	bf.Placeholder = "filter..."
	bf.CharLimit = 100
	bf.Width = fileListWidth - 8
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

func TestRenderStatusBar_StagedCount(t *testing.T) {
	t.Parallel()
	files := []fileItem{
		{change: git.FileChange{Path: "a.go", Staged: true}},
		{change: git.FileChange{Path: "b.go", Staged: false}},
	}
	m := newTestModel(t, files)
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "1 staged") {
		t.Errorf("status bar should show staged count, got %q", bar)
	}
}

func TestRenderStatusBar_SplitIndicator(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.splitDiff = true
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "split") {
		t.Error("status bar should show split indicator when splitDiff=true")
	}
}

func TestRenderStatusBar_StatusMsg(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.statusMsg = "committed!"
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "committed!") {
		t.Error("status bar should show status message")
	}
}

func TestRenderHelpBar_FileListMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	bar := m.renderHelpBar()
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
	bar := m.renderHelpBar()
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
	bar := m.renderHelpBar()
	for _, key := range []string{"↑/↓/^j/^k", "enter", "esc", "filter"} {
		if !strings.Contains(bar, key) {
			t.Errorf("branch help should contain %q", key)
		}
	}
}

func TestRenderHelpBar_FileListShowsBranches(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeFileList
	bar := m.renderHelpBar()
	if !strings.Contains(bar, "b") {
		t.Error("file list help should contain b for branches")
	}
}

func TestRenderBranchList(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.branches = []string{"main", "feature-a", "feature-b"}
	m.currentBranch = "main"
	m.branchCursor = 0
	out := m.renderBranchList(10)
	if !strings.Contains(out, "main") {
		t.Error("branch list should contain main")
	}
	if !strings.Contains(out, "feature-a") {
		t.Error("branch list should contain feature-a")
	}
	if !strings.Contains(out, "*") {
		t.Error("branch list should mark current branch with *")
	}
}

func TestRenderBranchItem_ContainsName(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	item := m.renderBranchItem("feature-branch", true, false)
	if !strings.Contains(item, "feature-branch") {
		t.Error("branch item should contain branch name")
	}
}

func TestRenderBranchItem_Current(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	item := m.renderBranchItem("main", false, true)
	if !strings.Contains(item, "*") {
		t.Error("current branch should have * prefix")
	}
}

func TestRenderFileItem_ShowsStats(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	item := fileItem{change: git.FileChange{Path: "main.go", Status: git.StatusModified, AddedLines: 12, DeletedLines: 3}}
	out := m.renderFileItem(item, false)
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

func TestHandleBranchesLoaded_Error(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	msg := branchesLoadedMsg{err: fmt.Errorf("permission denied")}
	result, _ := m.handleBranchesLoaded(msg)
	rm := result.(Model)
	if rm.mode != modeFileList {
		t.Error("should stay in file list mode on error")
	}
	if !strings.Contains(rm.statusMsg, "permission denied") {
		t.Errorf("statusMsg=%q, want error message", rm.statusMsg)
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
	result2, _ := rm.handleDiffLoaded(diffLoadedMsg{content: "old diff", index: 0})
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
	result, _ := m.handleDiffLoaded(diffLoadedMsg{content: "same diff", index: 0})
	rm := result.(Model)
	if rm.lastDiffContent != "same diff" {
		t.Error("cache should remain unchanged on duplicate")
	}
}

func TestBranchListScroll(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	// height=30, contentHeight=27, itemH=26 (minus filter bar).
	branches := make([]string, 40)
	for i := range branches {
		branches[i] = fmt.Sprintf("branch-%02d", i)
	}
	m.branches = branches
	m.branchCursor = 35
	m.branchOffset = 35 - 26 + 1 // 10

	out := m.renderBranchList(m.contentHeight())
	if !strings.Contains(out, "branch-35") {
		t.Error("branch list should show cursor branch when scrolled")
	}
	if strings.Contains(out, "branch-00") {
		t.Error("branch list should not show first branch when scrolled down")
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

func TestRenderBranchList_ShowsFilterBar(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev"}
	m.branchCursor = 0
	out := m.renderBranchList(10)
	// Should contain the match count
	if !strings.Contains(out, "2/2") {
		t.Error("branch list should show match count")
	}
}

func TestRenderBranchList_NoMatches(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev"}
	m.filteredBranches = []string{} // empty filter result
	m.branchFilter.SetValue("zzz")
	out := m.renderBranchList(10)
	if !strings.Contains(out, "no matches") {
		t.Error("should show 'no matches' placeholder")
	}
	if !strings.Contains(out, "0/2") {
		t.Error("should show 0/2 count")
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

func TestUpdateBranchMode_CreateMode_CtrlC_Cancels(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()
	m.branches = []string{"main"}

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyCtrlC})
	rm := result.(Model)
	if rm.branchCreating {
		t.Error("ctrl+c should cancel branch creation, not quit")
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
	bar := m.renderHelpBar()
	if !strings.Contains(bar, "^n") {
		t.Error("branch help should contain ^n for new branch")
	}
	if !strings.Contains(bar, "new") {
		t.Error("branch help should contain 'new' description")
	}
}

func TestRenderBranchCreateBar(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.branchCreating = true
	m.branchInput.Focus()
	bar := m.renderBranchCreateBar()
	if !strings.Contains(bar, "branch") {
		t.Error("create bar should contain 'branch' prompt")
	}
	if !strings.Contains(bar, "esc") {
		t.Error("create bar should show esc hint")
	}
	if !strings.Contains(bar, "enter") {
		t.Error("create bar should show enter hint")
	}
}

func TestView_BranchCreating_ShowsCreateBar(t *testing.T) {
	t.Parallel()
	// View() calls renderHeader() which needs a real repo for BranchName()
	// Use renderBranchCreateBar() directly to test view integration
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branchCreating = true
	m.branchInput.Focus()

	bar := m.renderBranchCreateBar()
	if !strings.Contains(bar, "new branch") {
		t.Error("create bar should show 'new branch' prompt")
	}
	if !strings.Contains(bar, "enter create") {
		t.Error("create bar should show 'enter create' hint")
	}
}
