package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

func reviewModel(t *testing.T, fixture string) Model {
	t.Helper()
	m := diffModel(t, fixture, 20)
	m.mode = modeFileList
	updated, _ := m.Update(key("r"))
	return updated.(Model)
}

func cursorOn(t *testing.T, m Model, typ DiffLineType, content string) Model {
	t.Helper()
	return m.setCursor(lineIndexOf(t, m.renderer.Parsed(), typ, content))
}

func TestBuildLineComment_OnAddedLine(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("buildLineComment reported failure on an added line")
	}
	if c.File != "src.ts" {
		t.Errorf("File = %q, want src.ts", c.File)
	}
	if c.Side != review.SideNew {
		t.Errorf("Side = %v, want new", c.Side)
	}
	if c.StartLine != 2 || c.EndLine != 2 {
		t.Errorf("lines = %d-%d, want 2-2", c.StartLine, c.EndLine)
	}
	if c.HunkIndex != 0 {
		t.Errorf("HunkIndex = %d, want 0", c.HunkIndex)
	}
	if c.Anchor != "  const user = await getUser(id)" {
		t.Errorf("Anchor = %q", c.Anchor)
	}
}

func TestBuildLineComment_OnRemovedLineUsesOldSide(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineRemoved, "  await persist(data)")

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.Side != review.SideOld {
		t.Errorf("Side = %v, want old", c.Side)
	}
	if c.StartLine != 11 {
		t.Errorf("StartLine = %d, want 11", c.StartLine)
	}
}

func TestBuildLineComment_ExcerptShowsTheChangeWithMarkers(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, _ := m.buildLineComment()

	if !strings.Contains(c.Excerpt, "-  const user = getUser(id)") {
		t.Errorf("excerpt missing the removed line:\n%s", c.Excerpt)
	}
	if !strings.Contains(c.Excerpt, "+  const user = await getUser(id)") {
		t.Errorf("excerpt missing the added line:\n%s", c.Excerpt)
	}
	if strings.Contains(c.Excerpt, "\x1b[") {
		t.Errorf("excerpt must be plain text, got ANSI:\n%q", c.Excerpt)
	}
	// The excerpt is the hunk, not the whole file.
	if strings.Contains(c.Excerpt, "persist(data)") {
		t.Errorf("excerpt leaked another hunk:\n%s", c.Excerpt)
	}
}

func TestBuildHunkComment_CoversTheWholeHunk(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.StartLine != 1 || c.EndLine != 5 {
		t.Errorf("hunk comment lines = %d-%d, want 1-5", c.StartLine, c.EndLine)
	}
	if c.HunkIndex != 0 {
		t.Errorf("HunkIndex = %d, want 0", c.HunkIndex)
	}
}

func TestBuildHunkComment_SecondHunk(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  persist(data)")

	c, _ := m.buildHunkComment()
	if c.StartLine != 8 || c.EndLine != 13 {
		t.Errorf("hunk comment lines = %d-%d, want 8-13", c.StartLine, c.EndLine)
	}
}

func TestBuildLineComment_OnUntrackedFile(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	parsed := ParseNewFile("alpha\nbeta\n")
	m.renderer = NewDiffRenderer(parsed, "new.ts", m.styles, m.theme, 80)
	m.rendererPath = "new.ts"
	m.files[0].change.Path = "new.ts"
	m = m.setCursor(1)

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.StartLine != 2 || c.Side != review.SideNew {
		t.Errorf("address = line %d side %v, want line 2 side new", c.StartLine, c.Side)
	}
	if !strings.Contains(c.Excerpt, "+beta") {
		t.Errorf("excerpt = %q", c.Excerpt)
	}
}

func TestBuildComment_FailsWithoutARenderer(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m.renderer = nil
	if _, ok := m.buildLineComment(); ok {
		t.Error("buildLineComment should fail with no diff loaded")
	}
	if _, ok := m.buildHunkComment(); ok {
		t.Error("buildHunkComment should fail with no diff loaded")
	}
}

func TestExcerpt_IsTruncatedForHugeHunks(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -1,400 +1,400 @@\n")
	for i := 0; i < 400; i++ {
		b.WriteString("+line\n")
	}
	parsed := ParseDiff(b.String())
	got := excerptFor(parsed, parsed.Hunks[0])

	if len(got) > maxExcerptChars+200 {
		t.Errorf("excerpt is %d chars, should be capped near %d", len(got), maxExcerptChars)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("a truncated excerpt should say so:\n%s", got[len(got)-200:])
	}
}

func TestDiffFixtureCoverage_CommentOnEveryFixture(t *testing.T) {
	for _, f := range testutil.Fixtures() {
		if f.Binary {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			m := diffModel(t, f.Name, 20)
			if m.renderer.LineCount() == 0 {
				t.Skip("no addressable lines")
			}
			m.mode = modeReview
			m.session = review.NewSession()
			m = m.setCursor(m.renderer.Parsed().FirstCommentableLine())
			if _, ok := m.buildLineComment(); !ok {
				t.Error("could not build a comment on this fixture")
			}
		})
	}
}

// A hunk that only deletes has no new-side range, so the comment has to take
// the old side. Without that the reference came out as ":L0", which is not a
// line — and nothing tested it: the reference tests build Comment values by
// hand, and nothing drove a C hunk comment over a pure deletion.
func TestHunkComment_APureDeletionTakesTheOldSide(t *testing.T) {
	t.Parallel()
	// A hunk that removes the first three lines and adds none. The deletion
	// has to be at the *top* of the file: with it further down the new-side
	// start is non-zero, so removing the branch this tests produced a
	// confidently wrong line rather than the ":L0" the symptom is named for,
	// and two of the three assertions below never ran.
	const raw = "@@ -1,3 +0,0 @@\n-gone one\n-gone two\n-gone three\n"
	_, th := testStyles()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "doomed.ts", Status: git.StatusModified}}})
	m.renderer = NewDiffRenderer(ParseDiff(raw), "doomed.ts", NewStyles(th), th, 80)
	m.rendererPath = "doomed.ts"
	m.diffCursor = 1 // a removed line

	c, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("no hunk comment could be built")
	}
	if c.Side != review.SideOld {
		t.Errorf("side = %v, want old — the hunk has no new-side lines", c.Side)
	}
	if c.StartLine < 1 {
		t.Errorf("StartLine = %d; a line number below 1 is not a line", c.StartLine)
	}
	// And so the reference, if any, is not nonsense.
	if ref := review.Reference(c); strings.Contains(ref, "L0") {
		t.Errorf("reference points at line zero: %q", ref)
	}
}

// Under -s the diff's new side is the index, not the working tree — so a
// new-side line number is as unresolvable as an old-side one.
//
// Stage a change, then edit above it: the diff still says line 3 while the
// code has moved to line 8. Keying the reference on Side alone emitted
// "@f.txt :L3", which lands on unrelated code — the exact failure that
// dropping old-side references was meant to avoid, still happening in
// `differ commit`, whose whole purpose is reviewing staged work.
func TestComment_StagedModeDoesNotClaimAWorktreeLine(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("f.txt", "a\nb\nc\n", "first")
	tr.Modify("f.txt", "a\nb\nCHANGED\n")
	tr.Stage("f.txt")
	// Now edit above the staged change, so the worktree and the index differ.
	tr.Modify("f.txt", "x1\nx2\nx3\nx4\nx5\na\nb\nCHANGED\n")

	// Which entry the cursor is on decides it, not the flag differ was
	// started with. git reports a file with both staged and unstaged changes
	// twice, staged first, so in *default* mode the cursor starts on an entry
	// whose diff was read with --cached. The first version of this test
	// asserted LocateLine for that and read as "the worktree case"; it was
	// asserting the wrong answer for the staged entry.
	for _, tc := range []struct {
		name       string
		stagedOnly bool
		staged     bool
		want       review.Locate
	}{
		{"default mode, the staged entry", false, true, review.LocateFile},
		{"default mode, the unstaged entry", false, false, review.LocateLine},
		{"staged only", true, true, review.LocateFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := liveModelStaged(t, tr, tc.stagedOnly)
			found := false
			for i, f := range m.files {
				if f.change.Path == "f.txt" && f.change.Staged == tc.staged {
					m.cursor, found = i, true
					break
				}
			}
			if !found {
				t.Skipf("no entry with staged=%v in this fixture", tc.staged)
			}
			if got := m.locateFor(review.SideNew); got != tc.want {
				t.Errorf("locate = %v, want %v", got, tc.want)
			}
		})
	}
}

// The old side never resolves, whatever the entry. Nothing tested this: the
// only SideOld case went through a deleted file, which returns LocateNone
// before the side is read.
func TestComment_TheOldSideNeverClaimsAWorktreeLine(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("f.txt", "a\nb\n", "first")
	tr.Modify("f.txt", "a\nCHANGED\n")

	m := liveModelStaged(t, tr, false)
	if len(m.files) == 0 {
		t.Fatal("nothing in the changeset")
	}
	if got := m.locateFor(review.SideOld); got != review.LocateFile {
		t.Errorf("locate = %v, want file — the old side describes the file before the change", got)
	}
}

// And the decision reaches the comment. Nothing checked that either: every
// test either built a Comment by hand or called locateFor directly, so
// dropping the field from both builders left the suite green.
func TestComment_TheBuildersRecordWhereItPoints(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("f.txt", "a\nb\nc\n", "first")
	tr.Modify("f.txt", "a\nCHANGED\nc\n")

	m := liveModelStaged(t, tr, false)
	m = settle(t, m, key("r"))
	if m.renderer == nil {
		t.Fatal("no diff on screen")
	}

	line, ok := m.buildLineComment()
	if !ok {
		t.Fatal("no line comment could be built")
	}
	if line.Locate == review.LocateUnknown {
		t.Error("a line comment carries no Locate, so it would degrade to file-only")
	}
	hunk, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("no hunk comment could be built")
	}
	if hunk.Locate == review.LocateUnknown {
		t.Error("a hunk comment carries no Locate")
	}
}

// A comment on a deleted file has nothing on disk to point at.
func TestComment_ADeletedFileHasNothingToPointAt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("gone.ts", "one\n", "first")
	tr.Delete("gone.ts")

	m := liveModelStaged(t, tr, false)
	if len(m.files) == 0 {
		t.Fatal("the deletion is not in the changeset")
	}
	if got := m.locateFor(review.SideOld); got != review.LocateNone {
		t.Errorf("locate = %v, want none", got)
	}
}

// liveModelStaged is liveModel with the staged-only flag under test.
func liveModelStaged(t *testing.T, tr *testutil.Repo, stagedOnly bool) Model {
	t.Helper()
	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := repo.ChangedFiles(stagedOnly, "")
	if err != nil {
		t.Fatal(err)
	}
	var untracked []string
	if !stagedOnly {
		if untracked, err = repo.UntrackedFiles(); err != nil {
			t.Fatal(err)
		}
	}
	th := theme.Themes["dark"]
	return NewModel(repo, config.Default(), changes, untracked, NewStyles(th), th, stagedOnly, "")
}

// Walking from one entry of a dual-listed file to the other re-resolves the
// comment into the other diff's line numbers. The claim about how precise
// those numbers are has to move with them.
//
// git lists a file with both staged and unstaged changes twice. Comment on
// the unstaged entry — worktree coordinates, a line reference — then move to
// the staged entry: the `--cached` diff loads, the comment is reanchored
// into index coordinates, and Locate still said the line resolved. The agent
// was handed `@f.txt :L3` for code that was at line 8 on disk, which is the
// exact failure the staged arm of locateFor exists to prevent.
func TestComment_ReanchoringAcrossEntriesRevisesTheClaim(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("f.txt", "a\nb\nc\n", "first")
	tr.Modify("f.txt", "a\nb\nCHANGED\n")
	tr.Stage("f.txt")
	tr.ExternalEdit("f.txt", "x1\nx2\nx3\nx4\nx5\na\nb\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 40})
	m = atEntry(t, m, "f.txt", false)
	m = settle(t, m, key("r"))

	m = cursorOn(t, m, LineContext, "CHANGED")
	updated, _ := m.updateReviewMode(key("c"))
	m = typeText(t, updated.(Model), "why?")
	updated, _ = m.updateReviewMode(key("ctrl+s"))
	m = updated.(Model)

	before := m.session.CommentsFor("f.txt")[0]
	if before.Locate != review.LocateLine {
		t.Fatalf("the comment was not written against worktree coordinates: %v", before.Locate)
	}

	// Walk to the staged entry, whose diff is --cached.
	m = settle(t, m, key("esc"))
	m = atEntry(t, m, "f.txt", true)

	after := m.session.CommentsFor("f.txt")[0]
	if after.StartLine != before.StartLine && after.Locate == review.LocateLine {
		t.Errorf("the comment moved from line %d to %d and still claims the "+
			"line resolves: reference %q",
			before.StartLine, after.StartLine, review.Reference(after))
	}
}

// atEntry puts the cursor on the named file's staged or unstaged entry,
// walking the list the way a user does.
func atEntry(t *testing.T, m Model, path string, staged bool) Model {
	t.Helper()
	want := -1
	for i, f := range m.files {
		if f.change.Path == path && f.change.Staged == staged {
			want = i
			break
		}
	}
	if want < 0 {
		t.Fatalf("no %s entry for %s in %d files", map[bool]string{true: "staged", false: "unstaged"}[staged], path, len(m.files))
	}
	for range len(m.files) {
		if m.cursor == want {
			return m
		}
		if m.cursor < want {
			m = settle(t, m, key("j"))
		} else {
			m = settle(t, m, key("k"))
		}
	}
	t.Fatalf("the cursor would not reach entry %d (it is at %d)", want, m.cursor)
	return m
}

// currentFileGone is the guard the comment builders stand behind, and
// neither of its bounds was tested: removing either left the suite green,
// while the doc comment said the only thing between a negative cursor and a
// panic inside a tea.Cmd was that this function checks.
func TestComment_AnOutOfRangeCursorPointsAtNothing(t *testing.T) {
	m := reviewModel(t, "multi_hunk")

	for _, cursor := range []int{-1, len(m.files), len(m.files) + 5} {
		probe := m
		probe.cursor = cursor
		if got := probe.locateFor(review.SideNew); got != review.LocateNone {
			t.Errorf("cursor %d gives %v, want LocateNone", cursor, got)
		}
		if !probe.currentFileGone() {
			t.Errorf("cursor %d is not reported as out of range", cursor)
		}
	}
}

// `differ commit` shows the index. When the working tree holds exactly the
// same bytes, a line number from that diff addresses the file on disk too —
// and blanket-degrading every staged entry meant the mode whose whole
// purpose is reviewing staged work could never point the agent at a line.
func TestComment_StagedAndIdenticalStillNamesTheLine(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")

	m := settle(t, liveModelStaged(t, tr, true), tea.WindowSizeMsg{Width: 120, Height: 40})
	m = settle(t, m, key("r"))

	if got := m.locateFor(review.SideNew); got != review.LocateLine {
		t.Errorf("locate = %v, want LocateLine — the worktree is the index", got)
	}

	// Edit the worktree without staging, and it degrades again.
	tr.ExternalEdit("src.ts", "a new first line\none\nSTAGED\n")
	if got := m.locateFor(review.SideNew); got != review.LocateFile {
		t.Errorf("locate = %v after an unstaged edit, want LocateFile — the "+
			"index line numbers no longer address the file on disk", got)
	}
}

// The comment takes its excerpt and anchor from the renderer and its path
// and Locate from the cursor. A diff load is a tea.Cmd, so holding j through
// the file list leaves the two disagreeing — and the comment then named one
// file while quoting another's hunk. With the reference now machine-
// actionable, that is a wrong edit rather than a confusing message.
func TestComment_RefusesWhileTheRendererIsAnotherFile(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "a1\na2\na3\n", "first")
	tr.CommitFile("b.txt", "b1\nb2\nb3\n", "second")
	tr.Modify("a.txt", "a1\nAAA\na3\n")
	tr.Modify("b.txt", "b1\nBBB\nb3\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 40})
	m = settle(t, m, key("r"))
	if m.rendererPath != m.currentFilePath() {
		t.Fatalf("the fixture did not settle: renderer=%q cursor=%q",
			m.rendererPath, m.currentFilePath())
	}

	// Move the cursor without letting the reload land, which is what happens
	// while git diff runs.
	moved, _ := m.updateReviewMode(key("n"))
	m = moved.(Model)
	if m.rendererPath == m.currentFilePath() {
		t.Skip("the diff loaded synchronously; there is no window to test")
	}

	if _, ok := m.buildLineComment(); ok {
		t.Error("a line comment was built from another file's diff")
	}
	if _, ok := m.buildHunkComment(); ok {
		t.Error("a hunk comment was built from another file's diff")
	}
}
