package feedback

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tmuxSession starts a detached session whose single pane appends everything
// it receives to a file, and returns the pane id and that file's path.
func tmuxSession(t *testing.T) (pane, outfile string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	outfile = filepath.Join(dir, "received.txt")
	name := "differ-test-" + strings.ReplaceAll(t.Name(), "/", "-")

	// Its own tmux server. On the shared one these sessions are visible to
	// internal/editor's tmux tests, which run as a parallel package and look
	// for a window running an editor: the two fought over one server and
	// failed each other intermittently.
	t.Setenv("TMUX_TMPDIR", shortTmuxDir(t))
	t.Setenv("TMUX", "")

	run := func(args ...string) string {
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-s", name, "-n", "w", "cat > "+outfile)
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })

	pane = run("list-panes", "-t", name+":w", "-F", "#{pane_id}")
	return pane, outfile
}

// waitForContent polls until the pane's output contains want. The receiving
// program buffers by line, so waiting for "any output" races the last line.
func waitForContent(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			last = string(data)
			if strings.Contains(last, want) {
				return last
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

func TestTmuxTarget_DeliversPayloadToThePane(t *testing.T) {
	pane, outfile := tmuxSession(t)
	tr := &tmuxTarget{target: pane, selfPane: "%none"}

	// Matches what review.FormatFeedback produces, trailing newline included.
	payload := "Review feedback: 1 comment\n\nFile: src.ts\nLine: 2 (new)\n\nComment:\nkeep this awaited\n"
	if err := tr.Send(context.Background(), payload); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	got := waitForContent(t, outfile, "keep this awaited")
	if !strings.Contains(got, "keep this awaited") {
		t.Errorf("pane did not receive the payload, got %q", got)
	}
	// Multiline structure must survive the trip.
	if !strings.Contains(got, "File: src.ts") || !strings.Contains(got, "Line: 2 (new)") {
		t.Errorf("payload arrived mangled:\n%q", got)
	}
}

func TestTmuxTarget_RejectsAnUnknownPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	tr := &tmuxTarget{target: "%999999", selfPane: "%none"}

	err := tr.Send(context.Background(), "payload")
	if err == nil {
		t.Fatal("expected an error for a pane that does not exist")
	}
	if !strings.Contains(err.Error(), "%999999") {
		t.Errorf("error should name the target: %v", err)
	}
}

func TestTmuxTarget_RefusesToSendToItself(t *testing.T) {
	pane, _ := tmuxSession(t)
	tr := &tmuxTarget{target: pane, selfPane: pane}

	err := tr.Send(context.Background(), "payload")
	if err == nil {
		t.Fatal("sending to differ's own pane should be refused")
	}
	if !strings.Contains(err.Error(), "own pane") {
		t.Errorf("error should explain the problem: %v", err)
	}
}

func TestTmuxTarget_DefaultTargetIsTheLastActivePane(t *testing.T) {
	// Both conditions. Guarding on $TMUX alone meant that with tmux off PATH
	// but the variable still set — a stripped container, a binary moved — this
	// failed where every other tmux test skipped.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if os.Getenv("TMUX") == "" {
		t.Skip("not running inside tmux")
	}
	tr, err := newTmuxTarget("")
	if err != nil {
		t.Fatalf("newTmuxTarget: %v", err)
	}
	tt, ok := tr.(*tmuxTarget)
	if !ok {
		t.Fatalf("got %T, want *tmuxTarget", tr)
	}
	if tt.target != defaultTmuxTarget {
		t.Errorf("default target = %q, want %q", tt.target, defaultTmuxTarget)
	}
	if tt.Name() != "tmux" {
		t.Errorf("Name() = %q", tt.Name())
	}
}

func TestTmuxTarget_NotInsideTmuxIsAnActionableError(t *testing.T) {
	t.Setenv("TMUX", "")
	_, err := newTmuxTarget("")
	if err == nil {
		t.Fatal("expected an error when not running inside tmux")
	}
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("error should suggest a working alternative: %v", err)
	}
}

func TestTmuxTarget_ResolvePaneReportsEmptyResultAsInvalid(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	// tmux exits 0 with empty output for an unknown pane, so an empty
	// resolution must be treated as failure.
	if _, err := resolveTmuxPane(context.Background(), "%999999"); err == nil {
		t.Error("an unresolvable target must be an error, not an empty pane id")
	}
}

// An explicit pane works from outside tmux; only the default needs a client.
//
// "{last}" means "the pane this session last looked at", which has no meaning
// without a client — but load-buffer and paste-buffer address the tmux
// *server*, so differ running in a plain terminal can send into a pane in
// tmux. Refusing that made the agent picker useless from outside tmux: you
// choose a pane by id and differ replies that it is not in tmux.
func TestTmuxTarget_AnExplicitPaneDoesNotNeedAClient(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")

	if _, err := newTmuxTarget("%1"); err != nil {
		t.Errorf("an explicit pane was refused outside tmux: %v", err)
	}
	if _, err := newTmuxTarget(""); err == nil {
		t.Error("the default target was accepted outside tmux, where it means nothing")
	}
}

// A tmux server that has gone away says so, and the error has to carry
// tmux's own words: it is what tells a pane that has exited (reopen the
// picker) from a server that has (report it and stop).
//
// exec.Cmd.Output puts stderr on ExitError.Stderr, whose Error() renders only
// "exit status 1", so without capturing it the two were indistinguishable.
func TestResolveTmuxPane_CarriesTmuxsOwnWords(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	// A socket name with no server behind it: tmux exits non-zero and
	// explains itself on stderr.
	t.Setenv("TMUX", "")
	dir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", dir)

	_, err := resolveTmuxPane(context.Background(), "%1")
	if err == nil {
		t.Fatal("resolving a pane with no server running succeeded")
	}
	if strings.Contains(err.Error(), "exit status") {
		t.Errorf("the error is %q, which is exec's words rather than tmux's", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "server") &&
		!strings.Contains(strings.ToLower(err.Error()), "connect") {
		t.Errorf("the error is %q and does not say the server is unreachable", err)
	}
}

// shortTmuxDir is a temporary directory with a short path.
//
// t.TempDir()'s name is built from the test's, and tmux puts its socket
// inside a `tmux-<uid>` subdirectory of this one: the result overran the
// 104-byte sun_path limit and tmux reported "File name too long".
func shortTmuxDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
