package testutil

import (
	"strings"
	"testing"
)

// Fixture diffs are hand-written, so they are only useful if git agrees they
// are real diffs. Each one is applied to its own pre-image and the result must
// match the declared post-image.
func TestFixtures_ApplyCleanly(t *testing.T) {
	for _, f := range Fixtures() {
		if f.Binary {
			continue // no text pre/post image to apply
		}
		t.Run(f.Name, func(t *testing.T) {
			r := NewRepo(t)
			if f.Before == "" {
				r.CommitFile(".keep", "", "init")
			} else {
				r.CommitFile(f.Path, f.Before, "init")
			}

			r.Write("fixture.patch", f.Diff)
			r.Git("apply", "--whitespace=nowarn", "fixture.patch")

			switch f.After {
			case "":
				if r.Exists(f.Path) {
					t.Errorf("%s should have been deleted", f.Path)
				}
			default:
				if got := r.Read(f.NewPath()); got != f.After {
					t.Errorf("post-image mismatch for %s:\n got: %q\nwant: %q", f.NewPath(), got, f.After)
				}
			}
		})
	}
}

func TestFixtures_CoverTheStatesReviewNeeds(t *testing.T) {
	want := []string{"multi_hunk", "new_file", "deleted_file", "renamed_file", "long_lines", "tabs_indent", "binary_file"}
	for _, name := range want {
		f := Fixture(t, name)
		if f.Name != name {
			t.Errorf("Fixture(%q).Name = %q", name, f.Name)
		}
	}
}

func TestFixture_UnknownNameFails(t *testing.T) {
	if _, ok := lookupFixture("no_such_fixture"); ok {
		t.Error("expected lookup to fail for unknown fixture")
	}
}

func TestFixture_MultiHunkHasSeveralHunks(t *testing.T) {
	f := Fixture(t, "multi_hunk")
	if n := strings.Count(f.Diff, "\n@@"); n < 2 {
		t.Errorf("multi_hunk has %d hunks, want at least 2", n+1)
	}
}

func TestApplyFixture_LeavesWorkingTreeChangeMatchingTheFixture(t *testing.T) {
	r := NewRepo(t)
	f := Fixture(t, "multi_hunk")

	r.ApplyFixture(f)

	// The pre-image is committed, the post-image is an unstaged change.
	if got := r.Read(f.Path); got != f.After {
		t.Errorf("working tree content = %q, want post-image", got)
	}
	if got := r.Git("show", "HEAD:"+f.Path); got != strings.TrimSpace(f.Before) {
		t.Errorf("committed content does not match pre-image:\n%s", got)
	}

	// git's own diff of the repo must contain the same changed lines.
	diff := r.Git("diff", "--no-ext-diff", "--color=never", "--", f.Path)
	for _, want := range []string{"-  const user = getUser(id)", "+  const user = await getUser(id)", "-  await persist(data)", "+  persist(data)"} {
		if !strings.Contains(diff, want) {
			t.Errorf("repo diff missing %q\ngot:\n%s", want, diff)
		}
	}
}

func TestAgentChangeset_ProducesAMixOfFileStates(t *testing.T) {
	r := NewRepo(t)

	r.AgentChangeset()

	states := map[string]string{} // path -> XY status code
	for _, l := range r.Status() {
		states[strings.TrimSpace(l[3:])] = l[:2]
	}

	want := map[string]string{
		"src/auth/login.ts":   " M", // unstaged edit
		"src/api/client.ts":   "M ", // staged edit
		"src/utils/format.ts": "??", // new, untracked
		"src/legacy/old.ts":   " D", // deleted in working tree
	}
	for path, code := range want {
		if got := states[path]; got != code {
			t.Errorf("%s status = %q, want %q (full status: %v)", path, got, code, states)
		}
	}
}

func TestStatus_PreservesTheStatusColumns(t *testing.T) {
	r := NewRepo(t)
	r.CommitFile("a.txt", "one\n", "init")
	r.ExternalEdit("a.txt", "two\n")

	lines := r.Status()
	if len(lines) != 1 {
		t.Fatalf("expected 1 status line, got %v", lines)
	}
	// Leading space of " M" must survive; trimming it loses staged-vs-unstaged.
	if lines[0] != " M a.txt" {
		t.Errorf("status line = %q, want %q", lines[0], " M a.txt")
	}
}

func TestStatus_EmptyForCleanRepo(t *testing.T) {
	r := NewRepo(t)
	r.CommitFile("a.txt", "one\n", "init")
	if got := r.Status(); len(got) != 0 {
		t.Errorf("clean repo status = %v, want empty", got)
	}
}
