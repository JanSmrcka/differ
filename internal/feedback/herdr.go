package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Finding agents in herdr, and sending a review to one.
//
// herdr is a multiplexer built for running coding agents side by side, and it
// can be asked what tmux makes differ reconstruct: `herdr agent list` names
// every agent, its state and its own session id, so there is no ps walk and
// no table of runner heuristics. Its integrations report themselves.
//
// differ shells out to the `herdr` CLI as it does to git. The socket is
// newline-delimited JSON and reading it directly buys nothing at a few
// milliseconds a call, while costing a long-lived connection inside a TUI.
//
// The listings print one line of JSON, `{"id":…,"result":{…}}`. There is no
// --json flag on them — passing one fails with "unknown option", whatever the
// published docs say. Failures are JSON on stderr with exit status 1.

// HerdrError is a failure herdr reported in its own terms. Code is what
// decides what happens next — agent_not_found reopens the picker,
// agent_blocked leaves the review pending — so it is kept rather than
// flattened into a string.
type HerdrError struct {
	Code    string
	Message string
}

func (e *HerdrError) Error() string {
	return fmt.Sprintf("herdr: %s (%s)", e.Message, e.Code)
}

// parseHerdrError reads herdr's JSON error from stderr, falling back to the
// raw text, then to the process error, when it is not JSON.
func parseHerdrError(stderr []byte, err error) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(stderr, &body) == nil && body.Error.Code != "" {
		return &HerdrError{Code: body.Error.Code, Message: body.Error.Message}
	}
	if said := strings.TrimSpace(string(stderr)); said != "" {
		return fmt.Errorf("herdr: %s", said)
	}
	return fmt.Errorf("herdr: %w", err)
}

type herdrAgent struct {
	Agent       string `json:"agent"`
	Status      string `json:"agent_status"`
	Cwd         string `json:"cwd"`
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"terminal_title_stripped"`
	Session     struct {
		Value string `json:"value"`
	} `json:"agent_session"`
}

type herdrWorkspace struct {
	ID       string `json:"workspace_id"`
	Label    string `json:"label"`
	Number   int    `json:"number"`
	Worktree *struct {
		RepoKey string `json:"repo_key"`
	} `json:"worktree"`
}

func parseHerdrAgents(out []byte) ([]herdrAgent, error) {
	var body struct {
		Result struct {
			Agents []herdrAgent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr agent list: %w", err)
	}
	return body.Result.Agents, nil
}

func parseHerdrWorkspaces(out []byte) ([]herdrWorkspace, error) {
	var body struct {
		Result struct {
			Workspaces []herdrWorkspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr workspace list: %w", err)
	}
	return body.Result.Workspaces, nil
}

// discoverHerdr is everything herdr discovery does once the two listings have
// answered. Pure, so the picker's whole input is tested without a server.
func discoverHerdr(agentsOut, workspacesOut []byte, self, selfWS string, repo RepoInfo) ([]Agent, error) {
	agents, err := parseHerdrAgents(agentsOut)
	if err != nil {
		return nil, err
	}
	workspaces, err := parseHerdrWorkspaces(workspacesOut)
	if err != nil {
		return nil, err
	}
	byID := map[string]herdrWorkspace{}
	repoOf := map[string]string{}
	for _, w := range workspaces {
		byID[w.ID] = w
		if w.Worktree != nil {
			repoOf[w.ID] = w.Worktree.RepoKey
		}
	}

	var found []Agent
	for _, a := range agents {
		// Never differ's own pane: it is not a destination.
		if a.PaneID == "" || a.PaneID == self {
			continue
		}
		found = append(found, Agent{
			Pane: a.PaneID, Tool: a.Agent, Dir: a.Cwd, Mux: "herdr",
			Workspace: byID[a.WorkspaceID].Label, WorkspaceID: a.WorkspaceID,
			State: a.Status, Title: a.Title, SessionID: a.Session.Value,
		})
	}
	sortHerdrAgents(found, selfWS, repo, repoOf)
	return found, nil
}

// stateRank orders herdr's agent states for the picker. An agent waiting for
// input is the one a review is for; one that is working is busy; one herdr
// cannot classify is still offered, last, which is how tmux treats every
// agent it finds.
func stateRank(state string) int {
	switch state {
	case "blocked", "idle", "done":
		return 0
	case "working":
		return 1
	}
	return 2
}

// sortHerdrAgents puts the most likely one first: waiting before busy, then
// in differ's own workspace, then working in the same repository, then by
// workspace number. repoOf maps a workspace id to its repo_key.
func sortHerdrAgents(agents []Agent, selfWS string, repo RepoInfo, repoOf map[string]string) {
	locality := func(a Agent) int {
		switch {
		case selfWS != "" && a.WorkspaceID == selfWS:
			return 0
		case repo.CommonDir != "" && repoOf[a.WorkspaceID] == repo.CommonDir,
			repo.Root != "" && underRoot(a.Dir, repo.Root):
			return 1
		}
		return 2
	}
	sort.SliceStable(agents, func(i, j int) bool {
		a, b := agents[i], agents[j]
		if ra, rb := stateRank(a.State), stateRank(b.State); ra != rb {
			return ra < rb
		}
		if la, lb := locality(a), locality(b); la != lb {
			return la < lb
		}
		return workspaceNumber(a.WorkspaceID) < workspaceNumber(b.WorkspaceID)
	})
}

// workspaceNumber reads "w12" as 12, so w2 sorts before w10; anything else
// sorts last.
func workspaceNumber(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "w"))
	if err != nil {
		return 1 << 30
	}
	return n
}

// promptTimeoutMS bounds `agent prompt --wait`. herdr's own gate is five
// seconds for the agent to start; this leaves room for the submission.
const promptTimeoutMS = 8000

// waitTimeoutMS bounds waiting for the agent to finish with a review. A turn
// longer than half an hour is not one differ needs to report on, and the
// bound keeps a forgotten wait from outliving its reason.
const waitTimeoutMS = 30 * 60 * 1000

// herdrTarget delivers a review as a prompt to a herdr agent.
//
// It submits. The tmux target deliberately does not press Enter, so it never
// executes anything in a pane that turns out not to be the agent — and that
// rule is a workaround for tmux being unable to tell an agent from a shell.
// herdr can: `agent prompt` is addressed to an agent, not a pane, and refuses
// with agent_blocked before sending anything when it cannot take a prompt.
// `pane send-text` is no substitute: it has no bracketed paste, so every
// newline is an Enter and a multiline review would submit its first line.
type herdrTarget struct {
	bin, self    string
	session      string
	fallbackPane string

	mu sync.Mutex
	// pane is where the last Send went, which is what Wait watches.
	pane string
}

func newHerdrTarget(cfg Config) (Target, error) {
	if cfg.HerdrTarget == "" && cfg.HerdrPane == "" {
		return nil, fmt.Errorf("no herdr agent chosen — choose one with %s", "A")
	}
	bin := herdrBin(cfg.Env)
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("herdr is not installed — set feedback_target to clipboard or stdout")
	}
	return &herdrTarget{
		bin: bin, self: cfg.Env.get("HERDR_PANE_ID"),
		session: cfg.HerdrTarget, fallbackPane: cfg.HerdrPane,
	}, nil
}

func (t *herdrTarget) Name() string { return "herdr" }

func (t *herdrTarget) Send(ctx context.Context, payload string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout+promptTimeoutMS*time.Millisecond)
	defer cancel()

	pane, err := t.resolvePane(ctx)
	if err != nil {
		return err
	}
	if pane == t.self {
		return fmt.Errorf("herdr pane %s is differ's own — choose the agent with %s", pane, "A")
	}
	_, err = runHerdr(ctx, t.bin, "agent", "prompt", pane, payload,
		"--wait", "--until", "working", "--until", "blocked",
		"--timeout", strconv.Itoa(promptTimeoutMS))
	var he *HerdrError
	if errors.As(err, &he) && he.Code == "agent_prompt_stalled" {
		// Submitted, and herdr did not see the agent start. The text is in
		// the agent's hands: reporting a failure would invite a retry, and
		// a retry would send the review twice.
		err = nil
	}
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.pane = pane
	t.mu.Unlock()
	return nil
}

// resolvePane finds the pane the chosen agent is in now: by its session id
// when herdr still lists it, else the pane it was in when chosen. A pane id
// is not stable — a pane moved to another workspace gets a new one — and
// herdr does not take a session id as a target, so it is looked up.
func (t *herdrTarget) resolvePane(ctx context.Context) (string, error) {
	if t.session == "" {
		return t.fallbackPane, nil
	}
	out, err := runHerdr(ctx, t.bin, "agent", "list")
	if err != nil {
		return "", err
	}
	agents, err := parseHerdrAgents(out)
	if err != nil {
		return "", err
	}
	for _, a := range agents {
		if a.Session.Value == t.session {
			return a.PaneID, nil
		}
	}
	if t.fallbackPane == "" {
		return "", &HerdrError{Code: "agent_not_found", Message: "the chosen agent's session is no longer running"}
	}
	return t.fallbackPane, nil
}

// Wait blocks until the agent the last Send reached is idle, done or blocked,
// and names which. herdr blocks server-side, so this is one process and no
// polling; cancelling ctx kills it.
//
// It must start after the prompt was seen working, which Send's --wait
// ensures: herdr does not track turns, and a wait begun while the agent was
// mid-task could resolve against the previous one.
func (t *herdrTarget) Wait(ctx context.Context) (string, error) {
	t.mu.Lock()
	pane := t.pane
	t.mu.Unlock()
	if pane == "" {
		return "", fmt.Errorf("nothing was sent to wait for")
	}
	out, err := runHerdr(ctx, t.bin, "agent", "wait", pane,
		"--until", "idle", "--until", "done", "--until", "blocked",
		"--timeout", strconv.Itoa(waitTimeoutMS))
	if err != nil {
		return "", err
	}
	var body struct {
		Result struct {
			Agent herdrAgent `json:"agent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return "", fmt.Errorf("herdr agent wait: %w", err)
	}
	return body.Result.Agent.Status, nil
}
