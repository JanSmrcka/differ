package editor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortDir is a temp directory under /tmp rather than t.TempDir().
//
// macOS caps a unix socket path at ~104 bytes, and Go's t.TempDir() sits
// under /var/folders/… with a long random name — nvim --listen there fails
// with "Failed to --listen: invalid argument".
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "dte")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// headlessNvim starts an nvim listening on a socket, with the given extra
// environment, and returns the socket path.
func headlessNvim(t *testing.T, env []string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	dir := shortDir(t)
	sock := filepath.Join(dir, "s")

	cmd := exec.Command("nvim", append([]string{"--clean", "--headless", "--listen", sock}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start nvim: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nvim never created %s", sock)
	return ""
}

// The keystone of reuse: an nvim inherits TMUX_PANE from the shell that
// started it, so its own environment is what ties a socket to a pane.
//
// The alternatives do not work. The socket filename carries the pid of a
// child nvim spawns, and that child has no tty, so neither pid nor tty can
// map a socket to a pane.
func TestQueryNvim_ReportsThePaneItRunsIn(t *testing.T) {
	t.Parallel()
	sock := headlessNvim(t, []string{"TMUX_PANE=%42"})

	got, err := queryNvim(context.Background(), sock, "$TMUX_PANE")
	if err != nil {
		t.Fatalf("queryNvim: %v", err)
	}
	if got != "%42" {
		t.Errorf("TMUX_PANE = %q, want %%42", got)
	}
}

func TestQueryNvim_ADeadSocketIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	sock := filepath.Join(shortDir(t), "nothing")

	// --remote-expr fails cleanly on an unreachable server. Plain --remote
	// does not: it prints "Editing locally" and starts a real nvim in the
	// foreground, which from a background goroutine would fight differ for
	// the terminal. That is why openInNvim uses --remote-expr.
	if _, err := queryNvim(context.Background(), sock, "1"); err == nil {
		t.Error("want an error for a socket nothing is listening on")
	}
}

// An nvim sitting on a hit-enter prompt accepts the connection and never
// answers. That must not wedge differ.
func TestQueryNvim_ASilentServerTimesOut(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	sock := filepath.Join(shortDir(t), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c // accept and say nothing
		}
	}()

	start := time.Now()
	if _, err := queryNvim(context.Background(), sock, "1"); err == nil {
		t.Error("want an error from a server that never answers")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %v, should give up around probeTimeout", elapsed)
	}
}

func TestOpenInNvim_OpensTheFileAtTheLine(t *testing.T) {
	t.Parallel()
	sock := headlessNvim(t, nil)
	dir := filepath.Dir(sock)
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := openInNvim(context.Background(), sock, target, 3); err != nil {
		t.Fatalf("openInNvim: %v", err)
	}

	name, err := queryNvim(context.Background(), sock, `expand("%:p")`)
	if err != nil {
		t.Fatal(err)
	}
	if name != target {
		t.Errorf("open buffer = %q, want %q", name, target)
	}
	line, err := queryNvim(context.Background(), sock, `line(".")`)
	if err != nil {
		t.Fatal(err)
	}
	if line != "3" {
		t.Errorf("cursor line = %q, want 3", line)
	}
}

// The acceptance criterion, measured rather than assumed: :drop reuses the
// window and leaves an unsaved buffer alone.
func TestOpenInNvim_AModifiedBufferIsNeverDiscarded(t *testing.T) {
	t.Parallel()
	sock := headlessNvim(t, nil)
	dir := filepath.Dir(sock)

	edited := filepath.Join(dir, "edited.txt")
	if err := os.WriteFile(edited, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := openInNvim(context.Background(), sock, edited, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := queryNvim(context.Background(), sock, `execute("normal! ggIUNSAVED")`); err != nil {
		t.Fatal(err)
	}

	windowsBefore, err := queryNvim(context.Background(), sock, `winnr("$")`)
	if err != nil {
		t.Fatal(err)
	}

	if err := openInNvim(context.Background(), sock, other, 1); err != nil {
		t.Fatalf("openInNvim: %v", err)
	}

	state, err := queryNvim(context.Background(), sock,
		`getbufvar(bufnr(fnameescape('`+edited+`')), "&modified") . "|" .`+
			`get(getbufline(bufnr(fnameescape('`+edited+`')), 1), 0, "<gone>")`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(state, "1|UNSAVED") {
		t.Errorf("the unsaved buffer did not survive: %q", state)
	}

	// With nvim's default hidden=1 the reuse must not pile up windows either.
	windowsAfter, err := queryNvim(context.Background(), sock, `winnr("$")`)
	if err != nil {
		t.Fatal(err)
	}
	if windowsAfter != windowsBefore {
		t.Errorf("window count went %s → %s; :drop should reuse the window",
			windowsBefore, windowsAfter)
	}
}

// Pressing e twice on the same file must not multiply windows.
func TestOpenInNvim_IsIdempotent(t *testing.T) {
	t.Parallel()
	sock := headlessNvim(t, nil)
	target := filepath.Join(filepath.Dir(sock), "t.txt")
	if err := os.WriteFile(target, []byte("1\n2\n3\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := openInNvim(ctx, sock, target, 2); err != nil {
		t.Fatal(err)
	}
	if err := openInNvim(ctx, sock, target, 5); err != nil {
		t.Fatal(err)
	}

	windows, err := queryNvim(ctx, sock, `winnr("$")`)
	if err != nil {
		t.Fatal(err)
	}
	if windows != "1" {
		t.Errorf("window count = %s, want 1", windows)
	}
	line, err := queryNvim(ctx, sock, `line(".")`)
	if err != nil {
		t.Fatal(err)
	}
	if line != "5" {
		t.Errorf("cursor line = %q, want 5", line)
	}
}

// A path with a quote in it must not break out of the vimscript string.
func TestOpenInNvim_HandlesAwkwardPaths(t *testing.T) {
	t.Parallel()
	sock := headlessNvim(t, nil)
	target := filepath.Join(filepath.Dir(sock), "it's a file.txt")
	if err := os.WriteFile(target, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := openInNvim(context.Background(), sock, target, 1); err != nil {
		t.Fatalf("openInNvim: %v", err)
	}
	name, err := queryNvim(context.Background(), sock, `expand("%:p")`)
	if err != nil {
		t.Fatal(err)
	}
	if name != target {
		t.Errorf("open buffer = %q, want %q", name, target)
	}
}
