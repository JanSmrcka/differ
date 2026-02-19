# CLAUDE.md

## Project

`differ` — a terminal UI git diff viewer built with Go + Bubble Tea. See PLAN.md for full specification.

## Build & Run

```bash
go mod tidy
make build        # → bin/differ
make install      # → $GOPATH/bin/differ
go run .          # run in current directory
go run . -s       # staged only
go run . log      # commit browser
```

## Test

```bash
make test
# Always test in a real git repo with actual changes.
# Create test files, stage some, leave some unstaged — verify all states render correctly.
```

## Architecture Rules

- **Git via shell**: use `os/exec.Command("git", ...)` for all git operations. Do NOT use go-git library. Set `cmd.Dir` to repo root for every command.
- **Bubble Tea pattern**: every TUI model must implement `Init()`, `Update()`, `View()`. Use commands (returning `tea.Cmd`) for async work like loading diffs. Never block in Update.
- **Styles in one place**: all lipgloss styles live in `internal/ui/styles.go`, derived from the active `theme.Theme`. UI code calls style functions, never creates styles inline.
- **Theme decoupled**: `internal/theme/` knows nothing about lipgloss rendering. It only defines color values. `styles.go` bridges theme → lipgloss.

## Code Style

- No global mutable state. Pass config and theme through structs.
- Error handling: return errors up, don't panic. Print user-friendly messages in `cmd/`.
- Keep functions short. If a function exceeds ~50 lines, split it.
- Use `internal/` for all packages — nothing is public API.

## Dependencies

Only these external dependencies:

```
github.com/charmbracelet/bubbletea    # TUI framework
github.com/charmbracelet/bubbles      # viewport, textinput
github.com/charmbracelet/lipgloss     # styling
github.com/alecthomas/chroma/v2       # syntax highlighting
github.com/spf13/cobra                # CLI
```

Do not add more dependencies without strong justification.

## UX Priorities

1. **Fast startup** — must feel instant. No loading screens. If there are no changes, print one line and exit.
2. **Readable diffs** — syntax highlighting must work correctly. Added/removed lines must have distinct background colors that are visible but not harsh.
3. **Keyboard flow** — vim-style (j/k/g/G/d/u). User should never need to reach for mouse. Mode switches must feel instant.
4. **Information density** — show file status, staged state, line numbers, diff content. Don't waste space on decorative elements.

## Common Tasks

### Adding a new keybinding

1. Add to the appropriate `update*Mode` method in `model.go`
2. Add to `renderHelp()` in the same file
3. Update PLAN.md keyboard shortcuts table

### Adding a new git operation

1. Add method to `Repo` struct in `internal/git/repo.go`
2. Test the underlying git command manually first
3. Handle errors — git commands can fail for many reasons

### Adding a new theme

1. Define color values in `internal/theme/theme.go`
2. Add to `Themes` map
3. That's it — styles.go picks it up automatically

## Gotchas

- **Chroma + lipgloss interaction**: when applying syntax highlighting colors on top of added/removed background colors, the Chroma foreground color must not override the background. Apply Chroma colors to text only, keep background from the diff line type.
- **Terminal width**: always respect `tea.WindowSizeMsg`. Never hardcode widths. The file list panel is fixed ~35 chars, diff panel gets the rest.
- **Viewport**: use `bubbles/viewport` for the diff panel. It handles scrolling, but you must call `viewport.SetContent()` whenever content changes and `viewport.GotoTop()` when switching files.
- **Unicode width**: use `lipgloss.Width()` for visual width, not `len()`. File paths and code can contain multi-byte characters.
- **Git diff encoding**: always use `--no-ext-diff --color=never` flags to get predictable output.
- **Untracked files**: these don't have a diff — read file content directly and format as new-file diff.

## Definition of Done (MVP)

- [ ] `differ` shows all changes (staged + unstaged + untracked) in two-panel TUI
- [ ] Syntax highlighting works for common languages (Go, JS/TS, Python, Rust, CSS, HTML, JSON, YAML, Markdown)
- [ ] File navigation with j/k, diff scrolling with j/k/d/u/g/G
- [ ] Stage/unstage individual files with Tab
- [ ] Stage all with `a`
- [ ] Commit flow: `c` → type message → Enter
- [ ] `differ log` shows recent commits, Enter to view diff
- [ ] `differ -r main` compares against a ref
- [ ] `differ -s` shows only staged
- [ ] Binary files handled (show message, don't crash)
- [ ] No changes → clean exit with message
- [ ] Not a git repo → error message
- [ ] Compiles and runs as single binary
