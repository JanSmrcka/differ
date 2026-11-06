package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Zellij: discovery and delivery.
//
// Both are one command. `zellij action list-panes --json --all` carries each
// pane's foreground command (pane_command) and working directory (pane_cwd),
// so unlike tmux there is no ps listing to walk — and `zellij action paste`
// is bracketed paste into a named pane, which is what the tmux target does
// with a buffer. Like tmux, it never presses Enter.

// zellijPaneID turns what Zellij gives out into the "terminal_N" form its
// commands take. ZELLIJ_PANE_ID is the bare number; list-panes ids are bare
// numbers too, with is_plugin saying which kind.
func zellijPaneID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := strconv.Atoi(s); err == nil {
		return "terminal_" + s
	}
	return s
}

func runZellij(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "zellij", append([]string{"action"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("zellij %s: %w", args[0], ctx.Err())
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("zellij is not installed or not on differ's PATH")
		}
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return nil, fmt.Errorf("zellij %s: %s", args[0], said)
		}
		return nil, fmt.Errorf("zellij %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

type zellijPane struct {
	ID          int    `json:"id"`
	IsPlugin    bool   `json:"is_plugin"`
	Exited      bool   `json:"exited"`
	Title       string `json:"title"`
	TabName     string `json:"tab_name"`
	TabPosition int    `json:"tab_position"`
	Command     string `json:"pane_command"`
	Cwd         string `json:"pane_cwd"`
}

func (p zellijPane) paneID() string { return "terminal_" + strconv.Itoa(p.ID) }

func listZellijPanes(ctx context.Context) ([]zellijPane, error) {
	out, err := runZellij(ctx, "list-panes", "--json", "--all")
	if err != nil {
		return nil, err
	}
	var panes []zellijPane
	if err := json.Unmarshal(out, &panes); err != nil {
		return nil, fmt.Errorf("zellij list-panes: unreadable output: %w", err)
	}
	return panes, nil
}

// discoverZellij is everything Agents does once zellij has answered. Pure, so
// the picker's input can be tested without a session.
func discoverZellij(listing []byte, self string, repo RepoInfo) ([]Agent, error) {
	var panes []zellijPane
	if err := json.Unmarshal(listing, &panes); err != nil {
		return nil, fmt.Errorf("zellij list-panes: unreadable output: %w", err)
	}
	return zellijAgents(panes, self, repo), nil
}

func zellijAgents(panes []zellijPane, self string, repo RepoInfo) []Agent {
	var found []Agent
	ownTab := ""
	for _, p := range panes {
		if !p.IsPlugin && p.paneID() == self {
			ownTab = p.TabName
		}
	}
	for _, p := range panes {
		// Plugins are bars, a held pane has already finished, and differ's
		// own pane is never a destination.
		if p.IsPlugin || p.Exited || p.paneID() == self {
			continue
		}
		tool := agentTool(p.Command)
		if tool == "" {
			continue
		}
		found = append(found, Agent{
			Pane: p.paneID(), Session: p.TabName, Window: strconv.Itoa(p.TabPosition),
			Tool: tool, Dir: p.Cwd, Mux: "zellij", Title: p.Title,
		})
	}
	SortAgents(found, ownTab, repo.Root)
	return found
}

type zellijMux struct{ self string }

func newZellijMux(env Env) *zellijMux {
	return &zellijMux{self: zellijPaneID(env.get("ZELLIJ_PANE_ID"))}
}

func (m *zellijMux) Name() string { return "zellij" }
func (m *zellijMux) Self() string { return m.self }
func (m *zellijMux) Searched() []string {
	return []string{
		"differ looks for claude, codex, gemini,",
		"copilot, opencode and aider in every pane",
		"of this zellij session.",
	}
}

func (m *zellijMux) Agents(ctx context.Context, repo RepoInfo) ([]Agent, error) {
	panes, err := listZellijPanes(ctx)
	if err != nil {
		return nil, err
	}
	return zellijAgents(panes, m.self, repo), nil
}

type zellijTarget struct{ pane, self string }

func newZellijTarget(cfg Config) (Target, error) {
	if _, err := exec.LookPath("zellij"); err != nil {
		return nil, fmt.Errorf("zellij is not installed — set feedback_target to clipboard or stdout")
	}
	pane := zellijPaneID(cfg.ZellijTarget)
	if pane == "" {
		// No "last pane" to fall back on, as tmux has: Zellij has no such
		// notion, so a guess here would paste into the wrong pane.
		return nil, fmt.Errorf("no zellij pane chosen — choose an agent with A, or set feedback_target to clipboard or stdout")
	}
	return &zellijTarget{pane: pane, self: zellijPaneID(cfg.Env.get("ZELLIJ_PANE_ID"))}, nil
}

func (t *zellijTarget) Name() string { return "zellij" }

func (t *zellijTarget) Send(ctx context.Context, payload string) error {
	if t.pane == t.self {
		return fmt.Errorf("zellij target %q is differ's own pane — choose the pane running your agent with A", t.pane)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	// Zellij says nothing when a pane id matches nothing, so look first. The
	// wording is what paneIsGone matches to reopen the picker.
	panes, err := listZellijPanes(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, p := range panes {
		if !p.IsPlugin && !p.Exited && p.paneID() == t.pane {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("zellij pane %s: pane not found", t.pane)
	}
	// "--" so a payload that begins with a dash is not read as a flag.
	_, err = runZellij(ctx, "paste", "--pane-id", t.pane, "--", payload)
	return err
}
