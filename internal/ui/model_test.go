package ui

import (
	"strings"
	"testing"

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
			got := buildFileItems(tt.changes, tt.untracked)
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
	return Model{
		files:  files,
		styles: NewStyles(th),
		theme:  th,
		cfg:    config.Default(),
		width:  120,
		height: 30,
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
	for _, key := range []string{"j/k", "enter", "esc", "q"} {
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

func TestUpdateBranchMode_Navigation(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	m.mode = modeBranchPicker
	m.branches = []string{"main", "dev", "feature"}
	m.branchCursor = 0

	result, _ := m.updateBranchMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	rm := result.(Model)
	if rm.branchCursor != 1 {
		t.Errorf("cursor=%d after j, want 1", rm.branchCursor)
	}

	result, _ = rm.updateBranchMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	rm = result.(Model)
	if rm.branchCursor != 0 {
		t.Errorf("cursor=%d after k, want 0", rm.branchCursor)
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
