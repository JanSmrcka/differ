package feedback

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Placeholder for the tmux target, completed in the tmux integration work.
func newTmuxTarget(target string) (Target, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux is not installed — set feedback_target to clipboard or stdout")
	}
	if os.Getenv("TMUX") == "" {
		return nil, fmt.Errorf("not running inside tmux — set feedback_target to clipboard or stdout")
	}
	return &commandTarget{name: "tmux", argv: []string{"tmux", "display-message", strings.TrimSpace(target)}}, nil
}
