# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`differ` — terminal UI git diff viewer built with Go + Bubble Tea. Two-panel layout: file list + syntax-highlighted diff.

## Build & Run

```bash
make build        # → bin/differ
make install      # → $GOPATH/bin/differ
go run .          # all changes (staged + unstaged + untracked)
go run . -s       # staged only
go run . -r main  # compare against ref
go run . -c       # open in commit mode
go run . log      # commit browser
go run . commit   # review staged + commit
```

## Test & Lint

```bash
make test              # go test ./...
golangci-lint run      # CI uses v2.10.1, no custom config
```

Unit tests exist across all packages. Test manually in a real git repo with staged, unstaged, and untracked files.

## Architecture

```
main.go → cmd/root.go (cobra commands)
             ├── internal/config   — Config struct, load/save ~/.config/differ/config.json
             ├── internal/git      — Repo struct, all git ops via os/exec
             ├── internal/theme    — color hex values only (no lipgloss)
             └── internal/ui
                   ├── model.go    — Model (diff viewer, 4 modes: file list / diff / commit / branch picker)
                   ├── log.go      — LogModel (commit log browser)
                   ├── diff.go     — diff parser + renderer
                   ├── highlight.go — Chroma syntax highlighting
                   └── styles.go   — all lipgloss styles, bridges theme → lipgloss
```

Two Bubble Tea models: `Model` (main diff viewer with file list/diff/commit/branch-picker modes) and `LogModel` (log browser). Both follow `Init()/Update()/View()`. All async work (git calls, AI commit messages) returned as `tea.Cmd` — never block in `Update`.

Version injected via ldflags at build (`-X .../cmd.version`), falls back to `debug.ReadBuildInfo()` for `go install`.

## Architecture Rules

- **Git via shell**: `os/exec.Command("git", ...)` for all git ops. No go-git. Set `cmd.Dir` to repo root.
- **Styles in one place**: all lipgloss styles in `styles.go`, derived from `theme.Theme`. No inline styles.
- **Theme decoupled**: `internal/theme/` defines color values only. `styles.go` bridges to lipgloss.

## Development Process

- **New features and bug fixes**: always use TDD (red-green-refactor). Use `/tdd` skill.
  1. Write failing test first
  2. Implement minimal code to pass
  3. Refactor while keeping tests green

## Code Style

- No global mutable state. Pass config/theme through structs.
- Return errors up, don't panic. User-friendly messages in `cmd/`.
- Keep functions under ~50 lines.
- Use `internal/` for all packages — nothing is public API.

## Dependencies

Only these external deps (don't add more without strong justification):

```
github.com/charmbracelet/bubbletea    # TUI framework
github.com/charmbracelet/bubbles      # viewport, textinput
github.com/charmbracelet/lipgloss     # styling
github.com/alecthomas/chroma/v2       # syntax highlighting
github.com/spf13/cobra                # CLI
```

## UX Priorities

1. **Fast startup** — instant feel. No changes → print one line and exit.
2. **Readable diffs** — syntax highlighting correct. Added/removed with distinct but non-harsh backgrounds.
3. **Keyboard flow** — vim-style (j/k/g/G/d/u). No mouse needed.
4. **Information density** — file status, staged state, line numbers, diff. No decorative waste.

## Common Tasks

### Adding a new keybinding

1. Add to appropriate `update*Mode` method in `model.go`
2. Add to `renderHelp()` in the same file

### Adding a new git operation

1. Add method to `Repo` in `internal/git/repo.go`
2. Test the git command manually first
3. Handle errors — git commands fail for many reasons

### Adding a new theme

1. Define color values in `internal/theme/theme.go`, add to `Themes` map
2. `styles.go` picks it up automatically

## Gotchas

- **Chroma + lipgloss**: apply Chroma foreground colors token-by-token, keep diff background from line type. Chroma must not override background.
- **Terminal width**: always respect `tea.WindowSizeMsg`. File list panel fixed ~35 chars (`fileListWidth`), diff gets the rest.
- **Viewport**: call `viewport.SetContent()` on content change, `viewport.GotoTop()` on file switch.
- **Unicode width**: use `lipgloss.Width()` not `len()`.
- **Git diff flags**: always `--no-ext-diff --color=never` for predictable output.
- **Untracked files**: no diff available — read file content directly, format as new-file diff via `RenderNewFile()`.
- **AI commit messages**: runs configurable `commit_msg_cmd` (default `claude -p`). Diff truncated to 8000 chars. Falls back gracefully if CLI unavailable.
