// Package testutil provides shared test infrastructure for differ: temporary
// git repositories in known states, raw diff fixtures and golden-file
// comparison.
//
// It deliberately does not import internal/git. Tests for that package live in
// package git itself, so a dependency here would create an import cycle.
// Callers pass Repo.Dir to git.NewRepo instead.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a temporary git repository, isolated from host git configuration.
// Builder methods fail the test on error, so callers can chain them.
type Repo struct {
	t   *testing.T
	Dir string
	env []string
}

// NewRepo creates an initialised git repository in a temp dir that is removed
// when the test ends. System, global and user config are neutralised so the
// developer's own git settings cannot change test outcomes.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	fakeHome := t.TempDir()

	r := &Repo{t: t, Dir: dir, env: repoEnv(fakeHome)}

	r.Git("init")
	r.writeLocalConfig(filepath.Join(fakeHome, "no-hooks"))
	return r
}

// writeLocalConfig appends the isolating settings to .git/config directly.
//
// It has to be repo-local rather than environmental: production code runs git
// through internal/git, which builds its own exec.Cmd and never sees this
// package's environment. It is written as a file rather than through four
// `git config` calls because those four were costing more than everything else
// in the suite put together — 484 of the 2006 git processes one `go test
// ./internal/ui` run spawned, against 121 repositories. Process startup is the
// expense here (`git --version` measures 13.6 ms on this machine against `git
// status`'s 15.6), so five processes per repository became one.
func (r *Repo) writeLocalConfig(hooksPath string) {
	r.t.Helper()
	// A repeated [core] section is valid: git merges sections by name, and keys
	// that appear once keep their only value. `git init` already wrote one.
	settings := "\n[user]\n\tname = test\n\temail = test@test.com\n" +
		"[commit]\n\tgpgsign = false\n" +
		"[core]\n\thooksPath = " + hooksPath + "\n"

	path := filepath.Join(r.Dir, ".git", "config")
	existing, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(existing, settings...), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
}

func repoEnv(fakeHome string) []string {
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

// Env returns the isolated environment used for git commands in this repo.
func (r *Repo) Env() []string { return append([]string(nil), r.env...) }

// Git runs a git command in the repo and returns its trimmed stdout.
// The test fails if the command fails.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write creates or overwrites a file in the repo, creating parent dirs.
func (r *Repo) Write(path, content string) *Repo {
	r.t.Helper()
	full := filepath.Join(r.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
	return r
}

// Read returns a file's content from the repo working tree.
func (r *Repo) Read(path string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dir, path))
	if err != nil {
		r.t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// Stage adds the given paths to the index. With no paths, stages everything.
func (r *Repo) Stage(paths ...string) *Repo {
	r.t.Helper()
	if len(paths) == 0 {
		paths = []string{"-A"}
	}
	r.Git(append([]string{"add"}, paths...)...)
	return r
}

// Commit commits whatever is currently staged.
func (r *Repo) Commit(msg string) *Repo {
	r.t.Helper()
	r.Git("commit", "-m", msg)
	return r
}

// CommitFile writes, stages and commits a file in one step.
func (r *Repo) CommitFile(path, content, msg string) *Repo {
	r.t.Helper()
	return r.Write(path, content).Stage(path).Commit(msg)
}

// Modify overwrites a tracked file, leaving the change unstaged.
func (r *Repo) Modify(path, content string) *Repo {
	r.t.Helper()
	return r.Write(path, content)
}

// Rename renames a tracked file via git, leaving the rename staged.
func (r *Repo) Rename(oldPath, newPath string) *Repo {
	r.t.Helper()
	full := filepath.Join(r.Dir, newPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", newPath, err)
	}
	r.Git("mv", oldPath, newPath)
	return r
}

// Delete removes a tracked file via git, leaving the deletion staged.
func (r *Repo) Delete(path string) *Repo {
	r.t.Helper()
	r.Git("rm", "-q", path)
	return r
}

// Untracked writes a file without adding it to the index.
func (r *Repo) Untracked(path, content string) *Repo {
	r.t.Helper()
	return r.Write(path, content)
}

// ExternalEdit simulates a change made by another process (a coding agent, an
// editor) while differ is running: the working tree moves, the index does not.
func (r *Repo) ExternalEdit(path, content string) *Repo {
	r.t.Helper()
	return r.Write(path, content)
}

// Exists reports whether a path exists in the repo working tree.
func (r *Repo) Exists(path string) bool {
	_, err := os.Stat(filepath.Join(r.Dir, path))
	return err == nil
}

// RemoveFile deletes a file from the working tree without touching the index,
// leaving an unstaged deletion.
func (r *Repo) RemoveFile(path string) *Repo {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.Dir, path)); err != nil {
		r.t.Fatalf("remove %s: %v", path, err)
	}
	return r
}

// Status returns `git status --porcelain` lines with their two status columns
// intact. Git trims its combined output, which would strip the leading space
// of an unstaged change on the first line and hide staged-vs-unstaged.
//
// Untracked files are listed individually (--untracked-files=all) rather than
// collapsed to their directory, matching the per-file granularity production
// code gets from `git ls-files --others`.
func (r *Repo) Status() []string {
	r.t.Helper()
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = r.Dir
	cmd.Env = r.env
	out, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("git status: %v", err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// GitIn runs a git command in an existing directory with the same isolation
// NewRepo applies, and returns its trimmed stdout. For callers that already
// hold a path rather than a *Repo.
func GitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = repoEnv(t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// NewBareRepo creates an empty bare repository in a temp dir and returns its
// path, for use as a push/pull remote.
func NewBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	cmd.Env = repoEnv(t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v\n%s", err, out)
	}
	return dir
}

// GitInAllowFail runs git in dir and returns its combined output without
// failing the test when git exits non-zero. A conflicting merge is the case
// this exists for: it is the outcome the test wants, and git reports it as a
// failure.
func GitInAllowFail(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = repoEnv(t.TempDir())
	out, _ := cmd.CombinedOutput()
	return string(out)
}
