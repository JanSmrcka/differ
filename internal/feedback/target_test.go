package feedback

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// skipWithoutClipboard skips when the machine has no clipboard command, which
// is the normal state of a headless CI runner.
func skipWithoutClipboard(t *testing.T) {
	t.Helper()
	if _, err := clipboardTarget(); err != nil {
		t.Skipf("no clipboard command available: %v", err)
	}
}

func TestResolve_Stdout(t *testing.T) {
	tr, err := Resolve(Config{Target: "stdout"})
	if err != nil {
		t.Fatalf("Resolve(stdout) failed: %v", err)
	}
	if tr.Name() != "stdout" {
		t.Errorf("Name() = %q, want stdout", tr.Name())
	}
}

func TestResolve_Clipboard(t *testing.T) {
	skipWithoutClipboard(t)
	tr, err := Resolve(Config{Target: "clipboard"})
	if err != nil {
		t.Fatalf("Resolve(clipboard) failed: %v", err)
	}
	if tr.Name() != "clipboard" {
		t.Errorf("Name() = %q, want clipboard", tr.Name())
	}
}

func TestResolve_EmptyTargetDefaultsToClipboard(t *testing.T) {
	skipWithoutClipboard(t)
	tr, err := Resolve(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Name() != "clipboard" {
		t.Errorf("default target = %q, want clipboard", tr.Name())
	}
}

// On a machine with no clipboard command, resolving must fail with advice
// rather than returning a target that would silently do nothing.
func TestResolve_WithoutAClipboardExplainsTheAlternatives(t *testing.T) {
	if _, err := clipboardTarget(); err == nil {
		t.Skip("this machine has a clipboard command")
	}
	_, err := Resolve(Config{Target: "clipboard"})
	if err == nil {
		t.Fatal("expected an error when no clipboard command exists")
	}
	for _, want := range []string{"stdout", "tmux"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should suggest %q: %v", want, err)
		}
	}
}

func TestResolve_UnknownTargetIsAnActionableError(t *testing.T) {
	_, err := Resolve(Config{Target: "carrier-pigeon"})
	if err == nil {
		t.Fatal("expected an error for an unknown target")
	}
	msg := err.Error()
	if !strings.Contains(msg, "carrier-pigeon") {
		t.Errorf("error should name the bad target: %s", msg)
	}
	// It should say what the valid options are rather than just refusing.
	for _, want := range []string{"clipboard", "stdout", "tmux"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should list %q as an option: %s", want, msg)
		}
	}
}

// A target that fails must fail loudly — callers rely on the error to keep
// comments pending.
func TestCommandTarget_ReportsFailure(t *testing.T) {
	tr := &commandTarget{name: "broken", argv: []string{"definitely-not-a-real-binary-xyz"}}
	err := tr.Send(context.Background(), "payload")
	if err == nil {
		t.Fatal("expected an error from a missing binary")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error should name the target: %v", err)
	}
}

func TestCommandTarget_WritesPayloadToStdin(t *testing.T) {
	// `cat` echoes stdin, proving the payload is piped rather than argv'd.
	tr := &commandTarget{name: "cat", argv: []string{"cat"}}
	if err := tr.Send(context.Background(), "piped payload"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
}

func TestCommandTarget_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tr := &commandTarget{name: "sleepy", argv: []string{"sleep", "10"}}
	err := tr.Send(ctx, "payload")
	if err == nil {
		t.Fatal("a cancelled context should abort the send")
	}
}

// The fake target is what the UI tests use; it must record what it was given.
func TestFake(t *testing.T) {
	f := NewFake()
	if err := f.Send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if got := f.Sent(); len(got) != 1 || got[0] != "first" {
		t.Errorf("Sent() = %v", got)
	}

	f.Err = errors.New("nope")
	if err := f.Send(context.Background(), "second"); err == nil {
		t.Error("fake should return its configured error")
	}
	if len(f.Sent()) != 1 {
		t.Error("a failed send must not be recorded as sent")
	}
}

// #4: while the TUI owns the alt screen, printing to stdout paints over it and
// is lost when the alt screen is torn down. The payload must be buffered and
// flushed after the program exits.
func TestStdoutTarget_BuffersUntilFlushed(t *testing.T) {
	tr := newStdoutTarget()

	if err := tr.Send(context.Background(), "first payload"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Send(context.Background(), "second payload"); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := tr.Flush(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"first payload", "second payload"} {
		if !strings.Contains(got, want) {
			t.Errorf("flushed output missing %q:\n%s", want, got)
		}
	}
}

func TestStdoutTarget_FlushIsIdempotent(t *testing.T) {
	tr := newStdoutTarget()
	if err := tr.Send(context.Background(), "once"); err != nil {
		t.Fatal(err)
	}

	var first, second strings.Builder
	_ = tr.Flush(&first)
	_ = tr.Flush(&second)

	if !strings.Contains(first.String(), "once") {
		t.Error("first flush lost the payload")
	}
	if strings.Contains(second.String(), "once") {
		t.Error("second flush repeated an already-flushed payload")
	}
}

func TestStdoutTarget_NothingToFlushWritesNothing(t *testing.T) {
	var out strings.Builder
	if err := newStdoutTarget().Flush(&out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("flush with no payloads wrote %q", out.String())
	}
}

func TestResolve_StdoutIsBuffered(t *testing.T) {
	tr, err := Resolve(Config{Target: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(Flusher); !ok {
		t.Errorf("stdout target %T must implement Flusher, or its payload is lost", tr)
	}
}
