package testutil

import (
	"embed"
	"testing"
)

// Raw diffs live in testdata rather than in Go string literals: a unified diff
// represents a blank context line as a single space, and a source formatter or
// editor that trims trailing whitespace would silently corrupt the fixture.
// Embedding keeps the bytes exact and lets any package use them regardless of
// its working directory.
//
//go:embed testdata/diffs/*.patch
var diffFS embed.FS

// DiffFixture is a diff in a known shape, paired with the file contents it was
// produced from. Every fixture's diff came from real git output.
type DiffFixture struct {
	Name string
	// Path is the file's pre-image path; for a rename, To holds the new path.
	Path string
	To   string
	// Before is the pre-image content, empty for an added file.
	Before string
	// After is the post-image content, empty for a deleted file.
	After string
	// Diff is the raw unified diff, exactly as `git diff` emitted it.
	Diff string
	// Binary marks a diff with no textual pre/post image.
	Binary bool
}

// NewPath returns the path the file has after the diff is applied.
func (f DiffFixture) NewPath() string {
	if f.To != "" {
		return f.To
	}
	return f.Path
}

const (
	tsBefore = `export function load(id: string) {
  const user = getUser(id)
  return user
}

function helper(a: number) {
  return a + 1
}

export function save(data: Data) {
  await persist(data)
  return true
}
`
	tsAfter = `export function load(id: string) {
  const user = await getUser(id)
  return user
}

function helper(a: number) {
  return a + 1
}

export function save(data: Data) {
  persist(data)
  return true
}
`
	goTabsBefore = "func main() {\n\tif ok {\n\t\treturn 1\n\t}\n}\n"
	goTabsAfter  = "func main() {\n\tif ok {\n\t\treturn 2\n\t}\n}\n"
)

func longLine(c string) string {
	s := `const banner = "`
	for i := 0; i < 300; i++ {
		s += c
	}
	return s + "\"\n"
}

// Fixtures returns every built-in diff fixture.
func Fixtures() []DiffFixture {
	return []DiffFixture{
		{
			Name:   "multi_hunk",
			Path:   "src.ts",
			Before: tsBefore,
			After:  tsAfter,
			Diff:   mustDiff("multi_hunk"),
		},
		{
			Name:  "new_file",
			Path:  "fresh.ts",
			After: "export const ok = true\n",
			Diff:  mustDiff("new_file"),
		},
		{
			Name:   "deleted_file",
			Path:   "src.ts",
			Before: tsBefore,
			Diff:   mustDiff("deleted_file"),
		},
		{
			Name:   "renamed_file",
			Path:   "src.ts",
			To:     "moved.ts",
			Before: tsBefore,
			After:  tsBefore,
			Diff:   mustDiff("renamed_file"),
		},
		{
			Name:   "long_lines",
			Path:   "long.ts",
			Before: longLine("y"),
			After:  longLine("z"),
			Diff:   mustDiff("long_lines"),
		},
		{
			Name:   "tabs_indent",
			Path:   "tabs.go",
			Before: goTabsBefore,
			After:  goTabsAfter,
			Diff:   mustDiff("tabs_indent"),
		},
		{
			Name:   "binary_file",
			Path:   "img.png",
			Diff:   mustDiff("binary_file"),
			Binary: true,
		},
	}
}

// Fixture returns the named fixture, failing the test if it does not exist.
func Fixture(t *testing.T, name string) DiffFixture {
	t.Helper()
	f, ok := lookupFixture(name)
	if !ok {
		t.Fatalf("no diff fixture named %q", name)
	}
	return f
}

func lookupFixture(name string) (DiffFixture, bool) {
	for _, f := range Fixtures() {
		if f.Name == name {
			return f, true
		}
	}
	return DiffFixture{}, false
}

func mustDiff(name string) string {
	data, err := diffFS.ReadFile("testdata/diffs/" + name + ".patch")
	if err != nil {
		panic("testutil: missing embedded diff fixture " + name + ": " + err.Error())
	}
	return string(data)
}
