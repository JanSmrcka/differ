package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The changed-file list is the primary navigator, and it had a plain bug: it
// stopped rendering at the panel height with no scrolling, so in a changeset
// larger than the panel the rest of the files were unreachable. Agent
// changesets are routinely that large.

// manyFiles builds a changeset larger than any panel.
func manyFiles(n int) []fileItem {
	files := make([]fileItem, 0, n)
	for i := range n {
		files = append(files, fileItem{change: git.FileChange{
			Path:       "src/file" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".ts",
			Status:     git.StatusModified,
			AddedLines: i,
		}})
	}
	return files
}

func TestFileList_ACursorPastThePanelScrollsIntoView(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 20 // a panel far smaller than the changeset

	// Walk to the last file the way a user would.
	for range 39 {
		updated, _ := m.updateFileListMode(key("j"))
		m = updated.(Model)
	}
	if m.cursor != 39 {
		t.Fatalf("cursor = %d, want 39", m.cursor)
	}

	shown := m.renderFileList(m.listHeight())
	last := m.files[39].change.Path
	if !strings.Contains(stripANSI(shown), "file"+string(rune('a'+39%26))+string(rune('0'+39/26))) {
		t.Errorf("the last file (%s) is not on screen:\n%s", last, stripANSI(shown))
	}
}

// The list showed filepath.Base only, so src/a/index.ts and src/b/index.ts
// were the same two words — in a changeset full of index.ts and page.tsx, the
// navigator told you nothing.
func TestFileList_FilesWithTheSameBasenameAreDistinguishable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		paths []string
		want  map[string]string
	}{
		{
			name:  "unique basenames stay short",
			paths: []string{"src/api/client.ts", "src/auth/login.ts"},
			want:  map[string]string{"src/api/client.ts": "client.ts", "src/auth/login.ts": "login.ts"},
		},
		{
			name:  "a clash adds one directory",
			paths: []string{"src/a/index.ts", "src/b/index.ts"},
			want:  map[string]string{"src/a/index.ts": "a/index.ts", "src/b/index.ts": "b/index.ts"},
		},
		{
			name:  "only as much as it takes",
			paths: []string{"x/deep/a/index.ts", "y/deep/b/index.ts", "z/other.ts"},
			want: map[string]string{
				"x/deep/a/index.ts": "a/index.ts",
				"y/deep/b/index.ts": "b/index.ts",
				"z/other.ts":        "other.ts",
			},
		},
		{
			name:  "a clash that goes all the way up",
			paths: []string{"a/x/index.ts", "b/x/index.ts"},
			want:  map[string]string{"a/x/index.ts": "a/x/index.ts", "b/x/index.ts": "b/x/index.ts"},
		},
		{
			name:  "one file needs nothing",
			paths: []string{"only/one.ts"},
			want:  map[string]string{"only/one.ts": "one.ts"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := shortNames(c.paths)
			for path, want := range c.want {
				if got[path] != want {
					t.Errorf("%s → %q, want %q", path, got[path], want)
				}
			}
		})
	}
}

// Through the renderer, not just the helper.
func TestFileList_TheRenderedListDisambiguatesPaths(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "src/a/index.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "src/b/index.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "src/api/client.ts", Status: git.StatusModified}},
	})
	got := stripANSI(m.renderFileList(10))

	for _, want := range []string{"a/index.ts", "b/index.ts"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list does not distinguish %q:\n%s", want, got)
		}
	}
	// The file with no clash keeps its short name.
	if !strings.Contains(got, "client.ts") {
		t.Errorf("an unambiguous file lost its short name:\n%s", got)
	}
	if strings.Contains(got, "src/api/client.ts") {
		t.Errorf("an unambiguous file shows its whole path:\n%s", got)
	}
}

// Review state is the column the issue asked for: how far you have got with
// each file, without opening it.
func TestFileList_ShowsReviewState(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "read.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "commented.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "sent.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "moved.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "untouched.ts", Status: git.StatusModified}},
	})
	m.mode = modeReview
	m.session = review.NewSession()
	m.session.MarkViewed("read.ts")
	m.session.Add(review.Comment{File: "commented.ts", StartLine: 1, EndLine: 1, Body: "x"})
	sentComment := m.session.Add(review.Comment{File: "sent.ts", StartLine: 1, EndLine: 1, Body: "y"})
	m.session.MarkSent([]string{sentComment.ID})
	m.session.MarkViewed("moved.ts")
	m.session.NoteChange("moved.ts")

	got := stripANSI(m.renderFileList(10))
	for _, want := range []string{"1", "sent", "changed"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list does not show %q:\n%s", want, got)
		}
	}
	// An unreviewed file needs no badge — that is the normal state.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "untouched.ts") && strings.Contains(line, "viewed") {
			t.Errorf("an unreviewed file was badged:\n%s", line)
		}
	}
}

// Outside review mode there is no review state to show, and the column would
// be dead space in the narrowest panel differ has.
func TestFileList_NoReviewColumnOutsideReviewMode(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.session = review.NewSession()
	m.session.MarkViewed("a.ts")
	m.session.NoteChange("a.ts")
	m.mode = modeFileList

	if got := stripANSI(m.renderFileList(10)); strings.Contains(got, "changed") {
		t.Errorf("review state leaked into the plain file list:\n%s", got)
	}
}

// Staged, then unstaged, then untracked — so the list reads in the order the
// user is going to act on it, and the staged block is not scattered through
// the rest.
func TestFileList_IsOrderedByStagedThenUnstagedThenUntracked(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("staged.ts", "one\n", "init")
	tr.CommitFile("unstaged.ts", "one\n", "second")
	tr.Modify("staged.ts", "one\ntwo\n")
	tr.Stage("staged.ts")
	tr.Modify("unstaged.ts", "one\ntwo\n")
	tr.Untracked("new.ts", "fresh\n")

	m := liveModel(t, tr)
	var order []string
	for _, f := range m.files {
		order = append(order, f.change.Path)
	}
	want := []string{"staged.ts", "unstaged.ts", "new.ts"}
	if len(order) != len(want) {
		t.Fatalf("files = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("files = %v, want %v", order, want)
			break
		}
	}
}

// A changeset that shrinks — the agent commits half its work — must not leave
// the window scrolled past the end of the list, showing nothing.
func TestFileList_TheWindowFollowsAShrinkingChangeset(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, manyFiles(40))
	m.mode = modeFileList
	m.height = 20
	m.cursor = 39
	m = m.clampFileScroll()
	if m.fileOffset == 0 {
		t.Fatal("the window never scrolled")
	}

	// Most of the changeset is committed away.
	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: manyFiles(3),
		keys:  map[string]string{},
	})
	m = updated.(Model)

	if got := stripANSI(m.renderFileList(m.listHeight())); strings.TrimSpace(got) == "" {
		t.Errorf("the list is empty after the changeset shrank (offset %d, %d files)", m.fileOffset, len(m.files))
	}
}

// Every row is exactly the panel width, with the right-hand column against the
// right edge. Ragged stats are what made the old list hard to scan, and a row
// wider than the panel would push the diff out of alignment.
func TestFileList_RowsAreExactlyThePanelWideWithTheRightColumnAligned(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified, AddedLines: 1, DeletedLines: 1}},
		{change: git.FileChange{Path: "src/deeply/nested/and/very/long/name.ts", Status: git.StatusModified, AddedLines: 1234, DeletedLines: 5678}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusAdded, AddedLines: 9}},
	})

	rows := strings.Split(m.renderFileList(10), "\n")
	if len(rows) != 3 {
		t.Fatalf("rendered %d rows, want 3", len(rows))
	}
	var ends []int
	for i, row := range rows {
		if got := lipgloss.Width(row); got != fileListWidth {
			t.Errorf("row %d is %d columns, want %d: %q", i, got, fileListWidth, stripANSI(row))
		}
		ends = append(ends, len(strings.TrimRight(stripANSI(row), " ")))
	}
	for i := 1; i < len(ends); i++ {
		if ends[i] != ends[0] {
			t.Errorf("the right column is ragged: rows end at %v\n%s", ends, stripANSI(strings.Join(rows, "\n")))
			break
		}
	}
}
