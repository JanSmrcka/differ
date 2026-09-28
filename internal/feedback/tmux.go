package feedback

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Delivering feedback into a pane running a coding agent.
//
// The payload goes via a tmux paste buffer rather than send-keys: load-buffer
// takes it on stdin, so nothing has to be escaped, and paste-buffer -p wraps
// it in bracketed paste, which makes a TUI treat a multiline payload as one
// paste instead of a sequence of keystrokes and submissions.
//
// The paste is not followed by Enter. The text lands in the agent's prompt and
// the user sends it, which keeps differ from executing anything in a pane that
// turns out not to be the agent.

// defaultTmuxTarget is the last active pane: in the common two-pane layout,
// and from a popup, that is the pane the user came from.
const defaultTmuxTarget = "{last}"

// bufferName keeps differ's payload out of the user's own paste buffers.
const bufferName = "differ-feedback"

type tmuxTarget struct {
	target string
	// selfPane is differ's own pane, so we can refuse to paste into it.
	selfPane string
}

func newTmuxTarget(target string) (Target, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux is not installed — set feedback_target to clipboard or stdout")
	}
	if os.Getenv("TMUX") == "" {
		return nil, fmt.Errorf("differ is not running inside tmux — set feedback_target to clipboard or stdout")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		target = defaultTmuxTarget
	}
	return &tmuxTarget{target: target, selfPane: os.Getenv("TMUX_PANE")}, nil
}

func (t *tmuxTarget) Name() string { return "tmux" }

func (t *tmuxTarget) Send(ctx context.Context, payload string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	pane, err := resolveTmuxPane(ctx, t.target)
	if err != nil {
		return err
	}
	if pane == t.selfPane {
		return fmt.Errorf("tmux target %q is differ's own pane — set tmux_target to the pane running your agent", t.target)
	}

	load := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", bufferName, "-")
	load.Stdin = strings.NewReader(payload)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// -d discards the buffer afterwards, -p uses bracketed paste.
	paste := exec.CommandContext(ctx, "tmux", "paste-buffer", "-d", "-p", "-b", bufferName, "-t", pane)
	if out, err := paste.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux paste-buffer to %s: %w: %s", pane, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// resolveTmuxPane turns a target expression into a concrete pane id.
//
// tmux exits 0 and prints nothing for a target it cannot resolve, so an empty
// result — not the exit status — is what marks an invalid target.
func resolveTmuxPane(ctx context.Context, target string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux target %q is not available: %w", target, err)
	}
	pane := strings.TrimSpace(string(out))
	if pane == "" {
		return "", fmt.Errorf("tmux target %q does not match a pane — check tmux_target", target)
	}
	return pane, nil
}
