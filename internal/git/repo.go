package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FileStatus represents the type of change for a file.
type FileStatus rune

const (
	StatusModified  FileStatus = 'M'
	StatusAdded     FileStatus = 'A'
	StatusDeleted   FileStatus = 'D'
	StatusRenamed   FileStatus = 'R'
	StatusCopied    FileStatus = 'C'
	StatusUntracked FileStatus = '?'
)

// FileChange represents a changed file in the working tree or index.
type FileChange struct {
	Path         string
	OldPath      string // non-empty for renames
	Status       FileStatus
	Staged       bool
	AddedLines   int
	DeletedLines int
}

// UpstreamInfo holds ahead/behind counts relative to the upstream branch.
type UpstreamInfo struct {
	Upstream string // e.g. "origin/main", empty if none
	Ahead    int
	Behind   int
}

// Commit represents a git commit entry.
type Commit struct {
	Hash    string
	Short   string
	Author  string
	Date    string
	Subject string
}

// Repo wraps git operations for a repository.
type Repo struct {
	dir string
}

// NewRepo validates the path is inside a git repo and returns a Repo.
func NewRepo(path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	r := &Repo{dir: abs}
	root, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %s", abs)
	}
	r.dir = strings.TrimSpace(root)
	return r, nil
}

// Dir returns the repository root directory.
func (r *Repo) Dir() string { return r.dir }

// HasCommits returns true if the repo has at least one commit.
func (r *Repo) HasCommits() bool {
	_, err := r.run("rev-parse", "HEAD")
	return err == nil
}

// BranchName returns the current branch name, or short hash if detached.
func (r *Repo) BranchName() string {
	out, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(out)
	if name == "HEAD" {
		// detached HEAD — return short hash
		hash, err := r.run("rev-parse", "--short", "HEAD")
		if err != nil {
			return "HEAD"
		}
		return strings.TrimSpace(hash)
	}
	return name
}

// ListBranches returns local branch names.
func (r *Repo) ListBranches() ([]string, error) {
	out, err := r.run("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// CreateBranch creates a new branch at the current HEAD.
func (r *Repo) CreateBranch(name string) error {
	_, err := r.run("branch", name)
	return err
}

// CheckoutBranch switches to the named branch.
func (r *Repo) CheckoutBranch(name string) error {
	_, err := r.run("switch", name)
	return err
}

// UpstreamStatus returns ahead/behind counts relative to the upstream branch.
// Returns zero-value UpstreamInfo if no upstream is configured.
func (r *Repo) UpstreamStatus() UpstreamInfo {
	upstream, err := r.run("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return UpstreamInfo{}
	}
	upstream = strings.TrimSpace(upstream)
	info := UpstreamInfo{Upstream: upstream}

	out, err := r.run("rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return info
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) == 2 {
		info.Ahead, _ = strconv.Atoi(parts[0])
		info.Behind, _ = strconv.Atoi(parts[1])
	}
	return info
}

// Push pushes to the upstream branch.
func (r *Repo) Push() error {
	_, err := r.runWithStderr("push")
	return err
}

// Pull pulls from the upstream branch using fast-forward only.
func (r *Repo) Pull() error {
	_, err := r.runWithStderr("pull", "--ff-only")
	return err
}

// ChangedFiles returns files changed in the working tree or index.
// If staged is true, only returns staged changes.
// If ref is non-empty, compares against that ref.
func (r *Repo) ChangedFiles(staged bool, ref string) ([]FileChange, error) {
	var files []FileChange

	if ref != "" {
		return r.changedFilesRef(ref)
	}

	// Staged changes
	var stagedFiles []FileChange
	var err error
	if r.HasCommits() {
		stagedFiles, err = r.diffNameStatus("--cached")
	} else {
		// No commits yet — diff staged against empty tree
		stagedFiles, err = r.diffNameStatusEmptyTree()
	}
	if err != nil {
		return nil, err
	}
	stagedStats, err := r.diffNumStat("--cached")
	if err != nil {
		return nil, err
	}
	applyStats(stagedFiles, stagedStats)
	for i := range stagedFiles {
		stagedFiles[i].Staged = true
	}
	files = append(files, stagedFiles...)

	if staged {
		return files, nil
	}

	// Unstaged changes
	unstagedFiles, err := r.diffNameStatus()
	if err != nil {
		return nil, err
	}
	unstagedStats, err := r.diffNumStat()
	if err != nil {
		return nil, err
	}
	applyStats(unstagedFiles, unstagedStats)
	files = append(files, unstagedFiles...)

	return files, nil
}

// UntrackedFiles returns paths of untracked files.
func (r *Repo) UntrackedFiles() ([]string, error) {
	out, err := r.run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DiffFile returns the raw diff for a single file.
func (r *Repo) DiffFile(path string, staged bool, ref string) (string, error) {
	args := []string{"diff", "--no-ext-diff", "--color=never"}
	if staged {
		args = append(args, "--cached")
	}
	if ref != "" {
		args = append(args, ref)
	}
	args = append(args, "--", path)
	return r.run(args...)
}

// ReadFileContent reads a file from the working tree.
func (r *Repo) ReadFileContent(path string) (string, error) {
	full := filepath.Join(r.dir, path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// StageFile stages a file.
func (r *Repo) StageFile(path string) error {
	_, err := r.run("add", "--", path)
	return err
}

// UnstageFile unstages a file.
func (r *Repo) UnstageFile(path string) error {
	if !r.HasCommits() {
		_, err := r.run("rm", "--cached", "--", path)
		return err
	}
	_, err := r.run("reset", "HEAD", "--", path)
	return err
}

// StageAll stages all changes.
func (r *Repo) StageAll() error {
	_, err := r.run("add", "-A")
	return err
}

// StagedDiff returns the full diff of staged changes.
func (r *Repo) StagedDiff() (string, error) {
	return r.run("diff", "--cached", "--no-ext-diff", "--color=never")
}

// Commit creates a commit with the given message.
func (r *Repo) Commit(msg string) error {
	_, err := r.run("commit", "-m", msg)
	return err
}

// Log returns the n most recent commits.
func (r *Repo) Log(n int) ([]Commit, error) {
	format := "%H%x00%h%x00%an%x00%ar%x00%s"
	out, err := r.run("log", "-"+strconv.Itoa(n), "--format="+format)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// CommitDiff returns the full diff for a commit.
// For the root commit (no parent), uses diff-tree against empty tree.
func (r *Repo) CommitDiff(hash string) (string, error) {
	out, err := r.run("diff", hash+"~1", hash, "--no-ext-diff", "--color=never")
	if err != nil {
		// Root commit — diff against empty tree
		return r.run("diff-tree", "-p", "--root", "--no-ext-diff", "--color=never", hash)
	}
	return out, nil
}

// CommitDiffFiles returns files changed in a commit.
func (r *Repo) CommitDiffFiles(hash string) ([]FileChange, error) {
	out, err := r.run("diff", hash+"~1", hash, "--name-status")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// run executes a git command and returns stdout.
func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// runWithStderr executes a git command and returns stdout.
// On error, includes stderr in the error message for better diagnostics.
func (r *Repo) runWithStderr(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// diffNameStatusEmptyTree lists staged files when there are no commits yet.
func (r *Repo) diffNameStatusEmptyTree() ([]FileChange, error) {
	// 4b825dc... is git's well-known empty tree hash
	out, err := r.run("diff-index", "--name-status", "--cached", "4b825dc642cb6eb9a060e54bf899d69f82c6b18f")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// diffNameStatus runs git diff --name-status with optional extra args.
func (r *Repo) diffNameStatus(extraArgs ...string) ([]FileChange, error) {
	args := append([]string{"diff", "--name-status", "--no-ext-diff", "--color=never"}, extraArgs...)
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func (r *Repo) diffNumStat(extraArgs ...string) (map[string]lineStats, error) {
	args := append([]string{"diff", "--numstat", "--no-ext-diff", "--color=never"}, extraArgs...)
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseNumStat(out), nil
}

// changedFilesRef returns files changed compared to a ref.
func (r *Repo) changedFilesRef(ref string) ([]FileChange, error) {
	out, err := r.run("diff", "--name-status", "--no-ext-diff", "--color=never", ref)
	if err != nil {
		return nil, err
	}
	files := parseNameStatus(out)
	stats, err := r.diffNumStat(ref)
	if err != nil {
		return nil, err
	}
	applyStats(files, stats)
	return files, nil
}

type lineStats struct {
	added   int
	deleted int
}

func parseNumStat(out string) map[string]lineStats {
	stats := make(map[string]lineStats)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		path := parseNumStatPath(parts[len(parts)-1])
		added := parseNumStatInt(parts[0])
		deleted := parseNumStatInt(parts[1])
		stats[path] = lineStats{added: added, deleted: deleted}
	}
	return stats
}

func parseNumStatPath(path string) string {
	if !strings.Contains(path, " => ") {
		return path
	}
	if strings.Contains(path, "{") && strings.Contains(path, "}") {
		open := strings.Index(path, "{")
		close := strings.LastIndex(path, "}")
		if open >= 0 && close > open {
			inside := path[open+1 : close]
			parts := strings.SplitN(inside, " => ", 2)
			if len(parts) == 2 {
				return path[:open] + parts[1] + path[close+1:]
			}
		}
	}
	parts := strings.SplitN(path, " => ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return path
}

func parseNumStatInt(s string) int {
	if s == "-" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func applyStats(files []FileChange, stats map[string]lineStats) {
	for i := range files {
		st, ok := stats[files[i].Path]
		if !ok {
			continue
		}
		files[i].AddedLines = st.added
		files[i].DeletedLines = st.deleted
	}
}

// parseNameStatus parses git diff --name-status output.
func parseNameStatus(out string) []FileChange {
	var files []FileChange
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		status := FileStatus(parts[0][0])
		fc := FileChange{Status: status, Path: parts[1]}
		if (status == StatusRenamed || status == StatusCopied) && len(parts) == 3 {
			fc.OldPath = parts[1]
			fc.Path = parts[2]
		}
		files = append(files, fc)
	}
	return files
}

// parseLog parses git log output with null-byte separators.
func parseLog(out string) []Commit {
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash:    parts[0],
			Short:   parts[1],
			Author:  parts[2],
			Date:    parts[3],
			Subject: parts[4],
		})
	}
	return commits
}
