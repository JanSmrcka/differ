package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// skipWithoutTmux keeps the tmux tests honest on a machine, and silent on CI,
// which has no tmux. Mirrors internal/feedback/tmux_test.go.
func skipWithoutTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

func TestParsePanes(t *testing.T) {
	t.Parallel()
	// Tab separated, because pane_current_path may contain spaces.
	out := strings.Join([]string{
		"%9\tnvim\tdiffer\t1\t1\t/Users/j/git/differ",
		"%11\tdiffer\tdiffer\t3\t1\t/Users/j/my code/thing",
		"", // tmux output ends with a newline
	}, "\n")

	got := parsePanes(out)
	want := []pane{
		{ID: "%9", Cmd: "nvim", Session: "differ", Window: "1", Index: "1", Path: "/Users/j/git/differ"},
		{ID: "%11", Cmd: "differ", Session: "differ", Window: "3", Index: "1", Path: "/Users/j/my code/thing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePanes =\n%+v\nwant\n%+v", got, want)
	}
}

func TestListPanes_FindsARealSession(t *testing.T) {
	skipWithoutTmux(t)
	name := sessionName(t)
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "w", "sleep 30").Run(); err != nil {
		t.Skipf("cannot start a tmux session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })

	panes, err := listPanes(context.Background(), 0)
	if err != nil {
		t.Fatalf("listPanes: %v", err)
	}
	for _, p := range panes {
		if p.Session == name {
			return
		}
	}
	t.Errorf("listPanes did not report session %q; got %+v", name, panes)
}

func TestCurrentSession_ResolvesAPaneToItsSession(t *testing.T) {
	skipWithoutTmux(t)
	name := sessionName(t)
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "w", "sleep 30").Run(); err != nil {
		t.Skipf("cannot start a tmux session: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })

	id, err := exec.Command("tmux", "list-panes", "-t", name+":w", "-F", "#{pane_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := currentSession(context.Background(), 0, strings.TrimSpace(string(id)))
	if err != nil {
		t.Fatalf("currentSession: %v", err)
	}
	if got != name {
		t.Errorf("currentSession = %q, want %q", got, name)
	}
}

// tmux exits 0 and prints nothing for a target it cannot resolve, so the empty
// result is what marks the failure — not the exit status.
func TestCurrentSession_AnUnknownPaneIsAnError(t *testing.T) {
	skipWithoutTmux(t)
	if _, err := currentSession(context.Background(), 0, "%999999"); err == nil {
		t.Error("want an error for a pane that does not exist")
	}
}

func TestEditorPanes(t *testing.T) {
	t.Parallel()
	// One nvim per session is this machine's real shape, which is exactly why
	// the session is a requirement and not a preference.
	all := []pane{
		{ID: "%1", Cmd: "nvim", Session: "other", Window: "1", Path: "/repo"},
		{ID: "%9", Cmd: "nvim", Session: "differ", Window: "1", Path: "/repo"},
		{ID: "%10", Cmd: "claude", Session: "differ", Window: "2", Path: "/repo"},
		{ID: "%11", Cmd: "differ", Session: "differ", Window: "3", Path: "/repo"},
	}

	cases := []struct {
		name    string
		panes   []pane
		session string
		want    []string // pane ids, in preference order
	}{
		{
			name:    "only editor panes in differ's own session",
			panes:   all,
			session: "differ",
			want:    []string{"%9"},
		},
		{
			name:    "a shell is never a candidate",
			panes:   []pane{{ID: "%1", Cmd: "zsh", Session: "differ"}},
			session: "differ",
			want:    nil,
		},
		{
			name: "vim and vi count as the same family",
			panes: []pane{
				{ID: "%1", Cmd: "vim", Session: "differ"},
				{ID: "%2", Cmd: "vi", Session: "differ"},
			},
			session: "differ",
			want:    []string{"%1", "%2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, p := range editorPanes(panesInScope(c.panes, c.session, ""), nil) {
				got = append(got, p.ID)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("editorPanes = %v, want %v", got, c.want)
			}
		})
	}
}

// Which of several candidates wins, and why.
func TestRankPanes(t *testing.T) {
	t.Parallel()
	const repo = "/Users/j/git/differ"

	cases := []struct {
		name  string
		panes []pane
		self  pane
		want  string
	}{
		{
			name: "a pane sitting in the repo beats one that is not",
			panes: []pane{
				{ID: "%5", Window: "1", Path: "/somewhere/else"},
				{ID: "%9", Window: "2", Path: repo},
			},
			want: "%9",
		},
		{
			name: "a pane below the repo root counts as in the repo",
			panes: []pane{
				{ID: "%5", Window: "1", Path: "/elsewhere"},
				{ID: "%9", Window: "2", Path: repo + "/internal/ui"},
			},
			want: "%9",
		},
		{
			// /Users/j/git/differ-demo must not count as inside
			// /Users/j/git/differ.
			name: "a sibling directory with a shared prefix is not inside the repo",
			panes: []pane{
				{ID: "%5", Window: "1", Path: repo + "-demo"},
				{ID: "%9", Window: "2", Path: repo},
			},
			want: "%9",
		},
		{
			name: "otherwise differ's own window wins",
			panes: []pane{
				{ID: "%5", Window: "1", Path: "/elsewhere"},
				{ID: "%9", Window: "3", Path: "/elsewhere"},
			},
			self: pane{Window: "3"},
			want: "%9",
		},
		{
			name: "otherwise the lowest window index wins",
			panes: []pane{
				{ID: "%5", Window: "4", Path: "/elsewhere"},
				{ID: "%9", Window: "1", Path: "/elsewhere"},
			},
			want: "%9",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := rankPanes(c.panes, repo, c.self)
			if len(got) == 0 {
				t.Fatal("rankPanes returned nothing")
			}
			if got[0].ID != c.want {
				t.Errorf("best = %s, want %s", got[0].ID, c.want)
			}
		})
	}
}

// The whole reuse path against a real tmux and a real nvim: differ in one
// pane, nvim in another of the same session, e opens the file there and
// focuses it — with no second editor started.
func TestReuse_EndToEndInARealSession(t *testing.T) {
	skipWithoutTmux(t)
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	repo, err := os.MkdirTemp("/tmp", "dtr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	target := filepath.Join(repo, "src.ts")
	if err := os.WriteFile(target, []byte("1\n2\n3\n4\n5\n6\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	name := sessionName(t)
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	// Window 1 runs nvim; window 2 stands in for differ.
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "ed",
		"-c", repo, "nvim", "--clean").Run(); err != nil {
		t.Skipf("cannot start tmux: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	if err := exec.Command("tmux", "new-window", "-t", name, "-n", "differ",
		"-c", repo, "sleep 60").Run(); err != nil {
		t.Fatalf("new-window: %v", err)
	}

	selfPane := tmuxQuery(t, "display-message", "-p", "-t", name+":differ", "#{pane_id}")
	edPane := tmuxQuery(t, "display-message", "-p", "-t", name+":ed", "#{pane_id}")
	waitForPaneCommand(t, edPane, "nvim")

	req := Request{
		File: "src.ts", Repo: repo, Line: 4,
		Env: Env{
			Editor: "nvim", InTmux: true, TmuxPane: selfPane,
			TmpDir: os.Getenv("TMPDIR"), XDGRuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
			User: os.Getenv("USER"),
		},
	}
	plan, err := Resolve(context.Background(), Config{}, req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plan.Strategy != StrategyReuse {
		t.Fatalf("Strategy = %q, want reuse (Desc %q)", plan.Strategy, plan.Desc)
	}
	if err := plan.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// No second editor was started.
	if got := tmuxQuery(t, "list-panes", "-s", "-t", name, "-F", "#{pane_current_command}"); strings.Count(got, "nvim") != 1 {
		t.Errorf("expected exactly one nvim in the session, got:\n%s", got)
	}
	// And the editor's window took focus.
	if active := tmuxQuery(t, "display-message", "-p", "-t", name, "#{window_name}"); active != "ed" {
		t.Errorf("active window = %q, want the editor's", active)
	}
}

func tmuxQuery(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		t.Fatalf("tmux %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// nvim takes a moment to replace the shell as the pane's running command.
func waitForPaneCommand(t *testing.T, paneID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_current_command}").Output()
		if err == nil && strings.TrimSpace(string(out)) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Skipf("pane %s never started running %s", paneID, want)
}

// new-window without a target lands in whichever session tmux considers
// current, which with several sessions open is not necessarily differ's. The
// window has to appear next to differ.
func TestWindowPlan_CreatesTheWindowInDiffersOwnSession(t *testing.T) {
	skipWithoutTmux(t)
	repo, err := os.MkdirTemp("/tmp", "dtw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })

	// Two sessions, so "the current one" is ambiguous. differ lives in the
	// first; the second is created later and is therefore the more recently
	// active, which is what an untargeted new-window would pick.
	mine, other := sessionName(t)+"-mine", sessionName(t)+"-other"
	for _, s := range []string{mine, other} {
		_ = exec.Command("tmux", "kill-session", "-t", s).Run()
		if err := exec.Command("tmux", "new-session", "-d", "-s", s, "-n", "w", "sleep 60").Run(); err != nil {
			t.Skipf("cannot start tmux: %v", err)
		}
		name := s
		t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	}
	selfPane := tmuxQuery(t, "display-message", "-p", "-t", mine+":w", "#{pane_id}")

	plan := windowPlan(Config{}, []string{"sleep", "30"}, Request{
		File: "src.ts", Repo: repo, Env: Env{InTmux: true, TmuxPane: selfPane},
	})
	if err := plan.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := tmuxQuery(t, "list-windows", "-t", other, "-F", "x"); strings.Count(got, "x") != 1 {
		t.Errorf("the other session gained a window:\n%s",
			tmuxQuery(t, "list-windows", "-t", other, "-F", "#{window_index} #{pane_current_command}"))
	}
	if got := tmuxQuery(t, "list-windows", "-t", mine, "-F", "x"); strings.Count(got, "x") != 2 {
		t.Errorf("differ's session should have gained a window, has:\n%s",
			tmuxQuery(t, "list-windows", "-t", mine, "-F", "#{window_index} #{pane_current_command}"))
	}
}

// Left to tmux the window comes out named "tmux", which says nothing in a
// status bar.
func TestWindowPlan_NamesTheWindowAfterTheEditor(t *testing.T) {
	skipWithoutTmux(t)
	name := sessionName(t)
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "w", "sleep 60").Run(); err != nil {
		t.Skipf("cannot start tmux: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	selfPane := tmuxQuery(t, "display-message", "-p", "-t", name+":w", "#{pane_id}")

	repo, err := os.MkdirTemp("/tmp", "dtn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })

	plan := windowPlan(Config{}, []string{"/bin/sleep", "30"}, Request{
		File: "src.ts", Repo: repo, Env: Env{InTmux: true, TmuxPane: selfPane},
	})
	if err := plan.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	names := tmuxQuery(t, "list-windows", "-t", name, "-F", "#{window_name}")
	if !strings.Contains(names, "sleep") {
		t.Errorf("window names = %q, want one named after the command", names)
	}
}

// "exit status 1" on its own says nothing. internal/feedback/tmux.go — the
// file this one deliberately copies — appends tmux's stderr, and so must
// this.
func TestRun_TheErrorCarriesTmuxsOwnWords(t *testing.T) {
	skipWithoutTmux(t)
	_, err := run(context.Background(), 0, "new-window", "-t", "nosuchxyz:", "true")
	if err == nil {
		t.Fatal("want an error for a session that does not exist")
	}
	// Assert the shape, not the wording: tmux says "can't find session" when
	// a server is up and "no server running on …" when one is not, and a CI
	// runner has no server. Either way something of tmux's own has to follow
	// the exit status.
	msg := err.Error()
	i := strings.Index(msg, "exit status")
	if i < 0 {
		t.Fatalf("error = %q, expected it to carry an exit status", msg)
	}
	if tail := strings.TrimSpace(msg[i+len("exit status"):]); len(tail) < 4 {
		t.Errorf("error = %q, want tmux's own complaint after the exit status", msg)
	}
}

// stderrOf is what makes those errors useful, and it needs no tmux at all.
func TestStderrOf(t *testing.T) {
	t.Parallel()
	_, err := exec.Command("sh", "-c", "echo 'the reason' >&2; exit 1").Output()
	if err == nil {
		t.Fatal("want a failing command")
	}
	if got := stderrOf(err); !strings.Contains(got, "the reason") {
		t.Errorf("stderrOf = %q, want the command's stderr", got)
	}
	if got := stderrOf(context.Canceled); got != "" {
		t.Errorf("stderrOf(non-exit error) = %q, want empty", got)
	}
}

// Which pane commands count as an editor has to be configurable: someone
// running helix or kakoune in a pane should be able to say so.
func TestEditorPanes_TheEditorListIsConfigurable(t *testing.T) {
	t.Parallel()
	panes := []pane{
		{ID: "%1", Cmd: "hx"},
		{ID: "%2", Cmd: "nvim"},
		{ID: "%3", Cmd: "zsh"},
	}

	cases := []struct {
		name  string
		allow []string
		want  []string
	}{
		{name: "empty falls back to the built-in set", want: []string{"%2"}},
		{name: "an explicit list replaces it", allow: []string{"hx"}, want: []string{"%1"}},
		{name: "several", allow: []string{"hx", "nvim"}, want: []string{"%1", "%2"}},
		{name: "a shell can be named, however unwise", allow: []string{"zsh"}, want: []string{"%3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, p := range editorPanes(panes, c.allow) {
				got = append(got, p.ID)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("editorPanes = %v, want %v", got, c.want)
			}
		})
	}
}

// Reuse is scoped to differ's own session by default, but a single-session
// layout may want it to reach anywhere.
func TestPanesInScope(t *testing.T) {
	t.Parallel()
	panes := []pane{
		{ID: "%1", Cmd: "nvim", Session: "mine"},
		{ID: "%2", Cmd: "nvim", Session: "other"},
	}
	cases := []struct {
		name, target string
		want         []string
	}{
		{name: "empty means differ's own session", want: []string{"%1"}},
		{name: "session means the same", target: "session", want: []string{"%1"}},
		{name: "any reaches every session", target: "any", want: []string{"%1", "%2"}},
		{name: "a name picks that session", target: "other", want: []string{"%2"}},
		{name: "a name that matches nothing yields nothing", target: "nope", want: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, p := range panesInScope(panes, "mine", c.target) {
				got = append(got, p.ID)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("panesInScope = %v, want %v", got, c.want)
			}
		})
	}
}

// editor_target "any" lets reuse reach an editor in another session, which
// also has to move the client — focusing a pane in a session the client is
// not attached to leaves the file open somewhere invisible.
//
// This asserts the choice rather than performing it. Running it would be
// unsafe: "any" means every session on the machine, and the first draft of
// this test opened its temporary file inside an unrelated nvim belonging to
// whoever happened to be logged in.
func TestReuse_TargetAnyReachesAnotherSession(t *testing.T) {
	skipWithoutTmux(t)
	repo, err := os.MkdirTemp("/tmp", "dta")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })

	mine, other := sessionName(t)+"-mine", sessionName(t)+"-other"
	for _, s := range []string{mine, other} {
		_ = exec.Command("tmux", "kill-session", "-t", s).Run()
	}
	if err := exec.Command("tmux", "new-session", "-d", "-s", mine, "-n", "differ",
		"-c", repo, "sleep 60").Run(); err != nil {
		t.Skipf("cannot start tmux: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", mine).Run() })
	// The pane runs sleep and editor_panes says sleep counts, so the scope
	// and ranking are exercised with no editor involved at all.
	if err := exec.Command("tmux", "new-session", "-d", "-s", other, "-n", "ed",
		"-c", repo, "sleep 60").Run(); err != nil {
		t.Skipf("cannot start tmux: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", other).Run() })

	selfPane := tmuxQuery(t, "display-message", "-p", "-t", mine+":differ", "#{pane_id}")
	edPane := tmuxQuery(t, "display-message", "-p", "-t", other+":ed", "#{pane_id}")
	waitForPaneCommand(t, edPane, "sleep")

	all, err := listPanes(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	self := pane{}
	for _, p := range all {
		if p.ID == selfPane {
			self = p
		}
	}

	// Scoped to differ's own session, the other session's editor is invisible.
	allow := []string{"sleep"}
	scoped := editorPanes(panesInScope(all, mine, ""), allow)
	for _, p := range scoped {
		if p.ID == edPane {
			t.Error("the default scope should not reach another session")
		}
	}

	// With "any" it is a candidate, and ranked ahead of anything in a session
	// that is neither differ's nor the repository's.
	any := rankPanes(editorPanes(panesInScope(all, mine, "any"), allow), repo, self)
	found := false
	for _, p := range any {
		if p.ID == edPane {
			found = true
		}
	}
	if !found {
		t.Errorf("target any did not reach %s; candidates: %+v", edPane, any)
	}
	// differ's own session runs sleep too, so it legitimately ranks first —
	// what matters is that the other session became reachable at all.
	if len(any) == 0 {
		t.Error("target any produced no candidates")
	}
}

// sessionName is a tmux session name unique to this test.
//
// These were fixed strings on the developer's real tmux server, killed in
// setup and again in Cleanup. Two concurrent `go test ./internal/editor/`
// runs, or a leftover session from an interrupted one, then fought over the
// same session: new-window failing, a reuse falling back to a new window, and
// nvim refusing a connection on a socket that had been killed underneath it —
// exactly the three symptoms of the flake seen in CI-less local runs.
// internal/feedback already does this; internal/editor did not.
func sessionName(t *testing.T) string {
	t.Helper()
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, t.Name())
	return "differ-test-" + safe + "-" + strconv.Itoa(os.Getpid())
}
