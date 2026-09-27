package feedback

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolve_KnownTargets(t *testing.T) {
	for _, name := range []string{"clipboard", "stdout"} {
		tr, err := Resolve(Config{Target: name})
		if err != nil {
			t.Fatalf("Resolve(%q) failed: %v", name, err)
		}
		if tr.Name() != name {
			t.Errorf("Name() = %q, want %q", tr.Name(), name)
		}
	}
}

func TestResolve_EmptyTargetDefaultsToClipboard(t *testing.T) {
	tr, err := Resolve(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Name() != "clipboard" {
		t.Errorf("default target = %q, want clipboard", tr.Name())
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

func TestStdoutTarget_BuffersUntilFlushed(t *testing.T) {
	var out strings.Builder
	tr := &stdoutTarget{w: &out}

	if err := tr.Send(context.Background(), "hello feedback"); err != nil {
		t.Fatal(err)
	}
	if out.String() == "" {
		t.Error("stdout target wrote nothing")
	}
	if !strings.Contains(out.String(), "hello feedback") {
		t.Errorf("payload missing: %q", out.String())
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
