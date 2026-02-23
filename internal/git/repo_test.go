package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitEnv returns env vars that fully isolate git from host config.
// HOME is set to fakeHome so ~/.gitconfig is never read.
func gitEnv(fakeHome string) []string {
	return []string{
		"HOME=" + fakeHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
		"PATH=" + os.Getenv("PATH"),
	}
}

// setupTestRepo creates a temp dir with git init + repo-local config,
// fully isolated from host git configuration.
func setupTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	fakeHome := t.TempDir()
	env := gitEnv(fakeHome)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	// Repo-local config overrides host settings for Repo.run() calls
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.com")
	run("config", "commit.gpgsign", "false")
	run("config", "core.hooksPath", filepath.Join(fakeHome, "no-hooks"))

	repo, err := NewRepo(dir)
	if err != nil {
		t.Fatalf("NewRepo: %v", err)
	}
	return repo
}

func writeFile(t *testing.T, repo *Repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo.Dir(), name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(os.Getenv("HOME"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func addCommit(t *testing.T, repo *Repo, filename, content, msg string) {
	t.Helper()
	writeFile(t, repo, filename, content)
	gitRun(t, repo.Dir(), "add", filename)
	gitRun(t, repo.Dir(), "commit", "-m", msg)
}

// --- Pure parser tests ---

func TestParseNameStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantPath string
		wantStat FileStatus
		wantOld  string
	}{
		{"modified", "M\tfile.go", 1, "file.go", StatusModified, ""},
		{"added", "A\tnew.go", 1, "new.go", StatusAdded, ""},
		{"deleted", "D\told.go", 1, "old.go", StatusDeleted, ""},
		{"renamed", "R100\told.go\tnew.go", 1, "new.go", StatusRenamed, "old.go"},
		{"copied", "C100\tsrc.go\tdst.go", 1, "dst.go", StatusCopied, "src.go"},
		{"empty", "", 0, "", 0, ""},
		{"whitespace", "  \t  ", 0, "", 0, ""},
		{"malformed_no_tab", "Mfile.go", 0, "", 0, ""},
		{"multiple", "M\ta.go\nA\tb.go", 2, "a.go", StatusModified, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseNameStatus(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("len=%d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if got[0].Path != tt.wantPath {
					t.Errorf("Path=%q, want %q", got[0].Path, tt.wantPath)
				}
				if got[0].Status != tt.wantStat {
					t.Errorf("Status=%c, want %c", got[0].Status, tt.wantStat)
				}
				if got[0].OldPath != tt.wantOld {
					t.Errorf("OldPath=%q, want %q", got[0].OldPath, tt.wantOld)
				}
			}
		})
	}
}

func TestParseLog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantSub string
	}{
		{"single", "abc123\x00abc\x00Alice\x002h ago\x00fix bug", 1, "fix bug"},
		{"multi", "h1\x00s1\x00A\x00d1\x00msg1\nh2\x00s2\x00B\x00d2\x00msg2", 2, "msg1"},
		{"empty", "", 0, ""},
		{"malformed", "only\x00three\x00parts", 0, ""},
		{"with_empty_lines", "\nabc\x00def\x00ghi\x00jkl\x00mno\n\n", 1, "mno"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseLog(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("len=%d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 && got[0].Subject != tt.wantSub {
				t.Errorf("Subject=%q, want %q", got[0].Subject, tt.wantSub)
			}
		})
	}
}

func TestParseNumStat(t *testing.T) {
	t.Parallel()
	got := parseNumStat("12\t3\tfile.go\n-\t-\tbinary.dat")
	if got["file.go"].added != 12 || got["file.go"].deleted != 3 {
		t.Fatalf("file.go stats mismatch: %+v", got["file.go"])
	}
	if got["binary.dat"].added != 0 || got["binary.dat"].deleted != 0 {
		t.Fatalf("binary stats mismatch: %+v", got["binary.dat"])
	}
}

// --- Integration tests ---

func TestNewRepo_Valid(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	if repo.Dir() == "" {
		t.Error("Dir() should not be empty")
	}
}

func TestNewRepo_NotGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := NewRepo(dir)
	if err == nil {
		t.Error("expected error for non-git dir")
	}
}

func TestHasCommits_Empty(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	if repo.HasCommits() {
		t.Error("empty repo should have no commits")
	}
}

func TestHasCommits_WithCommit(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "file.txt", "hello", "init")
	if !repo.HasCommits() {
		t.Error("should have commits after committing")
	}
}

func TestBranchName(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "x", "init")
	name := repo.BranchName()
	// Default branch is usually "main" or "master"
	if name == "" || name == "unknown" {
		t.Errorf("unexpected branch name: %q", name)
	}
}

func TestChangedFiles_Unstaged(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "f.txt", "v2")

	files, err := repo.ChangedFiles(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(files))
	}
	if files[0].Staged {
		t.Error("file should not be staged")
	}
	if files[0].AddedLines == 0 {
		t.Error("expected unstaged added lines > 0")
	}
}

func TestChangedFiles_Staged(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "f.txt", "v2")
	gitRun(t, repo.Dir(), "add", "f.txt")

	files, err := repo.ChangedFiles(false, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Staged && f.Path == "f.txt" {
			found = true
			if f.AddedLines == 0 {
				t.Error("expected staged added lines > 0")
			}
		}
	}
	if !found {
		t.Error("expected staged file f.txt")
	}
}

func TestChangedFiles_StagedOnly(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "f.txt", "v2")
	writeFile(t, repo, "other.txt", "new")
	gitRun(t, repo.Dir(), "add", "f.txt")

	files, err := repo.ChangedFiles(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(files))
	}
	if files[0].Path != "f.txt" || !files[0].Staged {
		t.Errorf("unexpected file: %+v", files[0])
	}
}

func TestChangedFiles_Ref(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	addCommit(t, repo, "f.txt", "v2", "update")

	files, err := repo.ChangedFiles(false, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].AddedLines == 0 {
		t.Error("expected ref diff added lines > 0")
	}
}

func TestUntrackedFiles(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "untracked.txt", "x")

	files, err := repo.UntrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "untracked.txt" {
		t.Errorf("expected [untracked.txt], got %v", files)
	}
}

func TestUntrackedFiles_Empty(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")

	files, err := repo.UntrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected no untracked files, got %v", files)
	}
}

func TestStageFile(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "f.txt", "v2")

	if err := repo.StageFile("f.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ := repo.ChangedFiles(true, "")
	if len(files) != 1 || !files[0].Staged {
		t.Error("file should be staged after StageFile")
	}
}

func TestUnstageFile(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "f.txt", "v2")
	if err := repo.StageFile("f.txt"); err != nil {
		t.Fatal(err)
	}

	if err := repo.UnstageFile("f.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ := repo.ChangedFiles(true, "")
	if len(files) != 0 {
		t.Error("file should be unstaged")
	}
}

func TestUnstageFile_NoCommits(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "f.txt", "v1")
	if err := repo.StageFile("f.txt"); err != nil {
		t.Fatal(err)
	}

	if err := repo.UnstageFile("f.txt"); err != nil {
		t.Fatal(err)
	}
	// After unstaging with no commits, should have no staged files
	files, _ := repo.ChangedFiles(true, "")
	if len(files) != 0 {
		t.Errorf("expected 0 staged files, got %d", len(files))
	}
}

func TestStageAll(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	writeFile(t, repo, "a.txt", "a")
	writeFile(t, repo, "b.txt", "b")

	if err := repo.StageAll(); err != nil {
		t.Fatal(err)
	}
	files, _ := repo.ChangedFiles(true, "")
	if len(files) != 2 {
		t.Errorf("expected 2 staged files, got %d", len(files))
	}
}

func TestDiffFile(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "line1\n", "init")
	writeFile(t, repo, "f.txt", "line1\nline2\n")

	diff, err := repo.DiffFile("f.txt", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestCommit(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "f.txt", "hello")
	if err := repo.StageFile("f.txt"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("test commit"); err != nil {
		t.Fatal(err)
	}
	if !repo.HasCommits() {
		t.Error("should have commits after Commit()")
	}
}

func TestReadFileContent(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "f.txt", "content")

	got, err := repo.ReadFileContent("f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "content" {
		t.Errorf("got %q, want %q", got, "content")
	}
}

func TestLog(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "first")
	addCommit(t, repo, "f.txt", "v2", "second")

	commits, err := repo.Log(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "second" {
		t.Errorf("most recent commit subject=%q, want %q", commits[0].Subject, "second")
	}
}

func TestCommitDiff(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	addCommit(t, repo, "f.txt", "v2", "update")

	commits, _ := repo.Log(1)
	diff, err := repo.CommitDiff(commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("expected non-empty commit diff")
	}
}

func TestListBranches(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	gitRun(t, repo.Dir(), "branch", "feature-a")
	gitRun(t, repo.Dir(), "branch", "feature-b")

	branches, err := repo.ListBranches()
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 3 {
		t.Fatalf("expected 3 branches, got %d: %v", len(branches), branches)
	}
}

func TestListBranches_NoCommits(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)

	branches, err := repo.ListBranches()
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 0 {
		t.Errorf("expected 0 branches, got %d", len(branches))
	}
}

func TestCheckoutBranch(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	gitRun(t, repo.Dir(), "branch", "other")

	if err := repo.CheckoutBranch("other"); err != nil {
		t.Fatal(err)
	}
	if got := repo.BranchName(); got != "other" {
		t.Errorf("branch=%q, want %q", got, "other")
	}
}

func TestCheckoutBranch_Dirty(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	// Create branch with different content for the same file
	gitRun(t, repo.Dir(), "branch", "other")
	gitRun(t, repo.Dir(), "checkout", "other")
	addCommit(t, repo, "f.txt", "v2-other", "other change")
	gitRun(t, repo.Dir(), "checkout", "-")
	// Now dirty the file so switch conflicts
	writeFile(t, repo, "f.txt", "dirty")

	err := repo.CheckoutBranch("other")
	if err == nil {
		t.Error("expected error switching with dirty worktree")
	}
}

func TestCommitDiffFiles(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	addCommit(t, repo, "f.txt", "v1", "init")
	addCommit(t, repo, "f.txt", "v2", "update")

	commits, _ := repo.Log(1)
	files, err := repo.CommitDiffFiles(commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "f.txt" {
		t.Errorf("Path=%q, want f.txt", files[0].Path)
	}
}
