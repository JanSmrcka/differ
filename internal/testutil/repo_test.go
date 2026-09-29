package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRepo_IsIsolatedGitRepo(t *testing.T) {
	r := NewRepo(t)

	if _, err := os.Stat(filepath.Join(r.Dir, ".git")); err != nil {
		t.Fatalf("expected .git in %s: %v", r.Dir, err)
	}
	if got := r.Git("config", "user.name"); got != "test" {
		t.Errorf("user.name = %q, want %q", got, "test")
	}
	if got := r.Git("config", "commit.gpgsign"); got != "false" {
		t.Errorf("commit.gpgsign = %q, want %q", got, "false")
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("fresh repo should be clean, got %q", got)
	}
}

func TestNewRepo_HomeIsNotTheRealHome(t *testing.T) {
	r := NewRepo(t)

	home := os.Getenv("HOME")
	for _, env := range r.Env() {
		if strings.HasPrefix(env, "HOME=") && env == "HOME="+home {
			t.Fatal("test repo env leaks the real HOME")
		}
	}
}

func TestCommitFile_ThenModify_LeavesUnstagedChange(t *testing.T) {
	r := NewRepo(t)

	r.CommitFile("a.txt", "one\n", "add a")
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Fatalf("after commit, status = %q, want clean", got)
	}

	r.Modify("a.txt", "two\n")
	if got := r.Git("status", "--porcelain"); got != "M a.txt" {
		t.Errorf("status = %q, want %q", got, "M a.txt")
	}
}

func TestRepo_FileStates(t *testing.T) {
	tests := []struct {
		name  string
		build func(*Repo)
		want  string
	}{
		{
			name:  "renamed",
			build: func(r *Repo) { r.CommitFile("old.txt", "x\n", "init"); r.Rename("old.txt", "new.txt") },
			want:  "R  old.txt -> new.txt",
		},
		{
			name:  "deleted",
			build: func(r *Repo) { r.CommitFile("gone.txt", "x\n", "init"); r.Delete("gone.txt") },
			want:  "D  gone.txt",
		},
		{
			name:  "untracked",
			build: func(r *Repo) { r.CommitFile("a.txt", "x\n", "init"); r.Untracked("new.txt", "hi\n") },
			want:  "?? new.txt",
		},
		{
			name: "staged and unstaged in one file",
			build: func(r *Repo) {
				r.CommitFile("a.txt", "one\n", "init")
				r.Write("a.txt", "two\n").Stage("a.txt")
				r.Modify("a.txt", "three\n")
			},
			want: "MM a.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRepo(t)
			tc.build(r)
			if got := r.Git("status", "--porcelain"); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExternalEdit_MimicsAnAgentWrite(t *testing.T) {
	r := NewRepo(t)
	r.CommitFile("a.txt", "one\n", "init")

	r.ExternalEdit("a.txt", "agent wrote this\n")

	if got := r.Read("a.txt"); got != "agent wrote this\n" {
		t.Errorf("content = %q", got)
	}
	if got := r.Git("status", "--porcelain"); got != "M a.txt" {
		t.Errorf("status = %q, want unstaged modification", got)
	}
}

func TestGitIn_RunsIsolatedGitInAnArbitraryDir(t *testing.T) {
	r := NewRepo(t)
	r.CommitFile("a.txt", "one\n", "init")

	// Same repo, reached only by path — the shape callers outside this
	// package need when they already hold a directory.
	out := GitIn(t, r.Dir, "log", "--oneline")
	if !strings.Contains(out, "init") {
		t.Errorf("log = %q, want it to mention the commit", out)
	}
	if got := GitIn(t, r.Dir, "config", "--get", "user.email"); got != "test@test.com" {
		t.Errorf("user.email = %q", got)
	}
}

func TestNewBareRepo_CanBeUsedAsARemote(t *testing.T) {
	bare := NewBareRepo(t)
	r := NewRepo(t)
	r.CommitFile("a.txt", "one\n", "init")

	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-u", "origin", "HEAD")

	if got := GitIn(t, bare, "log", "--oneline"); !strings.Contains(got, "init") {
		t.Errorf("remote log = %q, want the pushed commit", got)
	}
}

// The four settings NewRepo writes have to be repo-local, not environmental.
// Production code runs git through internal/git, which builds its own
// exec.Cmd and does not carry this package's environment — so anything that
// only lived in Repo.env would stop protecting the test the moment the code
// under test ran git itself. core.hooksPath is the one that matters most: it
// points at a directory that does not exist, which is what stops a developer's
// own commit hooks from running inside the test suite.
func TestNewRepo_IsolatingConfigIsRepoLocal(t *testing.T) {
	t.Parallel()
	r := NewRepo(t)

	for _, tc := range []struct{ key, want string }{
		{"user.name", "test"},
		{"user.email", "test@test.com"},
		{"commit.gpgsign", "false"},
	} {
		if got := r.Git("config", "--local", "--get", tc.key); got != tc.want {
			t.Errorf("local %s = %q, want %q", tc.key, got, tc.want)
		}
	}

	hooks := r.Git("config", "--local", "--get", "core.hooksPath")
	if hooks == "" {
		t.Fatal("core.hooksPath is not set locally, so the developer's own hooks would run")
	}
	if _, err := os.Stat(hooks); !os.IsNotExist(err) {
		t.Errorf("core.hooksPath %q exists; it must point at nothing", hooks)
	}
}
