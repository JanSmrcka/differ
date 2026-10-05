package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/git"
)

// #49: n/p were the next and previous file, which is the pair search wants for
// its matches. Files moved to J/K, so n and p mean nothing in the diff yet.
func TestRebind_JAndKMoveBetweenFiles(t *testing.T) {
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
	})
	m.mode = modeDiff

	u, _ := m.updateDiffMode(key("J"))
	if got := u.(Model).cursor; got != 1 {
		t.Fatalf("J: file %d, want 1", got)
	}
	u, _ = u.(Model).updateDiffMode(key("K"))
	if got := u.(Model).cursor; got != 0 {
		t.Errorf("K: file %d, want 0", got)
	}
	for _, k := range []string{"n", "p"} {
		if u, _ := m.updateDiffMode(key(k)); u.(Model).cursor != 0 {
			t.Errorf("%s still moves between files", k)
		}
	}
}

// c is comment wherever it means anything, so commit is C in the file list.
func TestRebind_CommitIsCapitalC(t *testing.T) {
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified, Staged: true}}})
	m.mode = modeFileList

	if u, _ := m.updateFileListMode(key("c")); u.(Model).mode == modeCommit {
		t.Error("c still opens the commit input")
	}
	if u, _ := m.updateFileListMode(key("C")); u.(Model).mode != modeCommit {
		t.Errorf("C: mode %v, want modeCommit", u.(Model).mode)
	}
}

// git switch carries a tracked change to the new branch without a word when it
// does not conflict, so the picker asks before doing it.
func TestRebind_SwitchingWithTrackedChangesAsksTwice(t *testing.T) {
	m := branchModel(t, 120, 30, "master", "a", "other")
	m.branchDirty = true
	m.branchCursor = 2

	u, cmd := m.updateBranchMode(key("enter"))
	m = u.(Model)
	if cmd != nil {
		t.Fatal("the first enter switched without asking")
	}
	if !strings.Contains(m.statusMsg, "other") || !strings.Contains(m.statusMsg, "enter") {
		t.Errorf("the warning does not say what happens and how to go on: %q", m.statusMsg)
	}
	if _, cmd = m.updateBranchMode(key("enter")); cmd == nil {
		t.Error("the second enter did not switch")
	}

	// Moving disarms it: the confirmation was about that branch.
	m.branchConfirm = ""
	u, _ = m.updateBranchMode(key("enter"))
	u, _ = u.(Model).updateBranchMode(key("up"))
	if _, cmd = u.(Model).updateBranchMode(key("enter")); cmd != nil {
		t.Error("an enter after moving switched without asking again")
	}
}

func TestRebind_SwitchingACleanTreeDoesNotAsk(t *testing.T) {
	m := branchModel(t, 120, 30, "master", "other")
	m.branchDirty = false
	m.branchCursor = 1

	if _, cmd := m.updateBranchMode(key("enter")); cmd == nil {
		t.Error("a clean tree was asked to confirm")
	}
}
