package editor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Talking to tmux.
//
// Duplicated deliberately from internal/feedback/tmux.go. The two call sites
// share about a dozen lines and want different advice in their errors —
// feedback says "set feedback_target", this says "set editor_strategy" — so a
// shared package would have to parameterise the suggestion and end up worse
// than two plain copies. Extract to internal/tmux if a third consumer appears.

// actTimeout bounds a tmux call so a wedged server cannot hang the UI. Same
// figure and the same reason as feedback.sendTimeout.
const actTimeout = 5 * time.Second

// paneFormat is tab separated because pane_current_path may contain spaces.
const paneFormat = "#{pane_id}\t#{pane_current_command}\t#{session_name}\t" +
	"#{window_index}\t#{pane_index}\t#{pane_current_path}"

// pane is one tmux pane as differ cares about it.
type pane struct{ ID, Cmd, Session, Window, Index, Path string }

// Target is how this pane is addressed in a tmux command.
func (p pane) Target() string { return p.ID }

// Label is how this pane is named to the user.
func (p pane) Label() string { return p.Session + ":" + p.Window + "." + p.Index }

func run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, actTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func listPanes(ctx context.Context) ([]pane, error) {
	out, err := run(ctx, "list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}
	return parsePanes(out), nil
}

func parsePanes(out string) []pane {
	var panes []pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			continue
		}
		panes = append(panes, pane{
			ID: f[0], Cmd: f[1], Session: f[2], Window: f[3], Index: f[4], Path: f[5],
		})
	}
	return panes
}

// currentSession is the session differ itself is running in.
//
// tmux exits 0 and prints nothing for a target it cannot resolve, so an empty
// result — not the exit status — is what marks an invalid pane. The same trap
// is documented in internal/feedback/tmux.go.
func currentSession(ctx context.Context, selfPane string) (string, error) {
	if selfPane == "" {
		return "", fmt.Errorf("differ is not running inside tmux — set editor_strategy to inline")
	}
	out, err := run(ctx, "display-message", "-p", "-t", selfPane, "#{session_name}")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("tmux does not know pane %s — set editor_strategy to inline", selfPane)
	}
	return strings.TrimSpace(out), nil
}

// paneWindow is the "session:index" of the window holding a pane.
//
// new-window takes a window as its target and refuses a pane id outright
// ("can't specify pane here"), so the pane differ runs in has to be
// translated first.
func paneWindow(ctx context.Context, paneID string) (string, error) {
	out, err := run(ctx, "display-message", "-p", "-t", paneID, "#{session_name}:#{window_index}")
	if err != nil {
		return "", err
	}
	// tmux prints nothing and exits 0 for a target it cannot resolve.
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("tmux does not know pane %s", paneID)
	}
	return strings.TrimSpace(out), nil
}

// focusPane brings a pane to the front. Both commands are needed:
// select-pane alone does not change the active window, and select-window
// alone does not change the active pane within it.
func focusPane(ctx context.Context, target string) error {
	if _, err := run(ctx, "select-window", "-t", target); err != nil {
		return err
	}
	_, err := run(ctx, "select-pane", "-t", target)
	return err
}

// editorFamily is the set of pane commands differ will hand a file to. It is
// deliberately small: sending :drop to something that is not an editor would
// type it into a shell.
var editorFamily = map[string]bool{"nvim": true, "vim": true, "vi": true, "view": true}

// editorPanes are the panes in differ's own session that are running an
// editor.
//
// The session is a requirement, not a preference. A typical layout has an
// editor open in every session, so without it e could jump into a different
// project entirely.
func editorPanes(all []pane, session string) []pane {
	var out []pane
	for _, p := range all {
		if p.Session == session && editorFamily[p.Cmd] {
			out = append(out, p)
		}
	}
	return out
}

// rankPanes orders candidates best first.
//
// The repository is only a preference: :cd and autochdir move a pane's path,
// and making it a requirement would switch reuse off with no visible reason.
func rankPanes(panes []pane, repo string, self pane) []pane {
	ranked := append([]pane(nil), panes...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if in, jn := under(a.Path, repo), under(b.Path, repo); in != jn {
			return in
		}
		if am, bm := a.Window == self.Window, b.Window == self.Window; am != bm {
			return am
		}
		return windowNum(a.Window) < windowNum(b.Window)
	})
	return ranked
}

// under reports whether path is the root or inside it. The separator check
// matters: /repo-demo is not inside /repo.
func under(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/")
}

func windowNum(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1 << 30
	}
	return n
}

// windowPlan opens the editor in a new tmux window.
//
// -a -t pins the window to differ's own session, immediately after differ's
// window. Without a target tmux picks whichever session it considers current,
// which with more than one session open is not necessarily differ's — the
// window would then appear in another project entirely.
//
// argv goes as separate arguments, not one string: tmux documents that
// new-window with multiple arguments executes them directly, without sh -c,
// "to avoid issues with shell quoting". So a path containing spaces needs no
// escaping. No -d, because the user pressed e and wants to be there.
func windowPlan(argv []string, req Request) Plan {
	return Plan{
		Kind:     KindDetached,
		Strategy: StrategyWindow,
		Desc:     "opened " + req.File + " in a new window",
		run: func(ctx context.Context) error {
			args := []string{"new-window"}
			if req.Env.TmuxPane != "" {
				where, err := paneWindow(ctx, req.Env.TmuxPane)
				if err != nil {
					return err
				}
				args = append(args, "-a", "-t", where)
			}
			// Name the window after the editor. Left to tmux it comes out
			// called "tmux", which is no help in a status bar.
			args = append(args, "-n", filepath.Base(argv[0]), "-c", req.Repo)
			args = append(args, argv...)
			_, err := run(ctx, args...)
			return err
		},
	}
}
