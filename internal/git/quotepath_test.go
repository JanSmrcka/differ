package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git quotes a path with non-ASCII bytes by default — "\305\276lu..." for
// žluťoučký.ts — so every path differ read came back mangled: the file list
// showed the escaped form, and asking git for that file's diff matched
// nothing and produced an empty diff.
func TestRepo_NonASCIIPathsSurviveIntact(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", ".")
	name := "žluťoučký.ts"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo, err := NewRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.ChangedFiles(false, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Logf("path from ChangedFiles: %q", f.Path)
		if f.Path != name {
			t.Errorf("path is mangled: got %q, want %q", f.Path, name)
		}
		d, err := repo.DiffFile(f.Path, false, "")
		if err != nil {
			t.Errorf("DiffFile(%q) failed: %v", f.Path, err)
		} else if d == "" {
			t.Errorf("DiffFile(%q) returned an empty diff", f.Path)
		}
	}
}
