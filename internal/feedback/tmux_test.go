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

	run := func(args ...string) string {
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	run("new-session", "-d", "-s", name, "-n", "w", "cat > "+outfile)
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })

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
