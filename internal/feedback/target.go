// Package feedback delivers review feedback out of differ.
//
// A target is anything that can accept a block of text: the clipboard, stdout,
// or a tmux pane running a coding agent. Targets are deliberately dumb — they
// take a finished payload and hand it somewhere. Deciding what to send, and
// what to do when a send fails, stays with the caller.
package feedback

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// sendTimeout bounds a delivery attempt so a wedged clipboard helper or tmux
// server cannot hang the UI.
const sendTimeout = 5 * time.Second

// Target delivers a feedback payload somewhere.
type Target interface {
	// Name is the target's configuration name, used in messages to the user.
	Name() string
	// Send delivers the payload. A non-nil error means nothing was delivered.
	Send(ctx context.Context, payload string) error
}

// Config selects and configures a target.
type Config struct {
	// Target names the delivery mechanism: clipboard, stdout, tmux, herdr or zellij.
	Target string
	// TmuxTarget is the pane tmux feedback goes to. See the tmux target.
	TmuxTarget string
	// HerdrTarget is the herdr agent's own session id, which survives a pane
	// move, and HerdrPane the pane it was in when chosen — the fallback.
	// Their own keys, not TmuxTarget: a "w2:p2" in tmux_target is a config
	// that is wrong rather than unset.
	HerdrTarget string
	HerdrPane   string
	// ZellijTarget is the pane zellij feedback goes to, as "terminal_N".
	ZellijTarget string
	// Env is the environment the target reads; the zero value is the
	// process's own.
	Env Env
}

// Available lists the target names a user may configure.
func Available() []string { return []string{"clipboard", "stdout", "tmux", "herdr", "zellij"} }

// Watcher is a target that can tell when the agent it last delivered to has
// finished with it: idle, done or blocked. Asserted like Flusher, because
// only herdr can be asked — tmux cannot tell an agent from a shell.
type Watcher interface {
	// Seen is the state the last successful Send saw the agent reach:
	// "working", "blocked", or "" when it saw nothing — a stalled prompt,
	// which was delivered but left the agent idle, so a Wait would match
	// that at once and report an answer nobody gave.
	Seen() string
	Wait(ctx context.Context) (string, error)
}

// Resolve builds the configured target. An empty name means clipboard, which
// works everywhere and needs no setup.
func Resolve(cfg Config) (Target, error) {
	switch cfg.Target {
	case "", "clipboard":
		return clipboardTarget()
	case "stdout":
		return newStdoutTarget(), nil
	case "tmux":
		return newTmuxTarget(cfg.TmuxTarget)
	case "herdr":
		return newHerdrTarget(cfg)
	case "zellij":
		return newZellijTarget(cfg)
	default:
		return nil, fmt.Errorf("unknown feedback target %q — set feedback_target to one of: %s",
			cfg.Target, strings.Join(Available(), ", "))
	}
}

// clipboardTarget picks the platform's clipboard command. Shelling out keeps
// the dependency list unchanged and behaves correctly over SSH, where an
// in-process clipboard library would silently write to the wrong machine.
func clipboardTarget() (Target, error) {
	for _, candidate := range clipboardCommands() {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return &commandTarget{name: "clipboard", argv: candidate}, nil
		}
	}
	return nil, fmt.Errorf("no clipboard command found (tried %s) — set feedback_target to stdout or tmux",
		strings.Join(clipboardCommandNames(), ", "))
}

func clipboardCommands() [][]string {
	if runtime.GOOS == "darwin" {
		return [][]string{{"pbcopy"}}
	}
	return [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}
}

func clipboardCommandNames() []string {
	var names []string
	for _, c := range clipboardCommands() {
		names = append(names, c[0])
	}
	return names
}

// commandTarget pipes the payload to a command's stdin.
type commandTarget struct {
	name string
	argv []string
}

func (t *commandTarget) Name() string { return t.name }

func (t *commandTarget) Send(ctx context.Context, payload string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, t.argv[0], t.argv[1:]...)
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return fmt.Errorf("%s: %w: %s", t.name, err, detail)
		}
		return fmt.Errorf("%s: %w", t.name, err)
	}
	return nil
}

// Flusher is a target that holds payloads until the terminal is free.
type Flusher interface {
	Flush(w io.Writer) error
}

// stdoutTarget buffers payloads rather than writing them immediately.
//
// differ runs in the alternate screen buffer, so printing during a session
// paints over the TUI and is discarded when the alt screen is torn down — the
// feedback would be reported as sent and then lost. The caller flushes after
// the program exits.
type stdoutTarget struct {
	mu       sync.Mutex
	payloads []string
}

func newStdoutTarget() *stdoutTarget { return &stdoutTarget{} }

func (t *stdoutTarget) Name() string { return "stdout" }

func (t *stdoutTarget) Send(_ context.Context, payload string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.payloads = append(t.payloads, payload)
	return nil
}

// Flush writes and clears everything buffered so far.
func (t *stdoutTarget) Flush(w io.Writer) error {
	t.mu.Lock()
	pending := t.payloads
	t.payloads = nil
	t.mu.Unlock()

	for _, p := range pending {
		if _, err := fmt.Fprintln(w, p); err != nil {
			return err
		}
	}
	return nil
}

// Fake records payloads instead of delivering them, for tests.
type Fake struct {
	mu   sync.Mutex
	sent []string
	// Err, when set, makes every Send fail.
	Err error
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Send(_ context.Context, payload string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.sent = append(f.sent, payload)
	return nil
}

// Sent returns the payloads delivered so far.
func (f *Fake) Sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	copy(out, f.sent)
	return out
}
