# differ

Terminal UI git diff viewer built with Go and Bubble Tea. Two-panel layout: file list + syntax-highlighted diff preview.

## Install

```bash
go install github.com/jansmrcka/differ@latest
```

Or build from source:

```bash
make build    # → bin/differ
make install  # → $GOPATH/bin/differ
```

## Usage

```bash
differ            # all changes (staged + unstaged + untracked)
differ -s         # staged only
differ -r main    # compare against ref
differ -c         # open in commit mode
differ log        # browse recent commits
differ commit     # review staged + commit
```

## Keyboard Shortcuts

### File List

| Key | Action |
|-----|--------|
| `j/k` | navigate files |
| `enter` / `l` | view diff |
| `tab` | stage/unstage file |
| `a` | stage all |
| `c` | commit (AI-generated message via `claude`) |
| `e` | open in editor (nvim via tmux) |
| `g/G` | first/last file |
| `q` | quit |

### Diff View

| Key | Action |
|-----|--------|
| `j/k` | scroll |
| `d/u` | half page down/up |
| `g/G` | top/bottom |
| `n/p` | next/prev file |
| `tab` | stage/unstage |
| `e` | open in editor |
| `esc` / `h` | back to file list |

### Commit Mode

| Key | Action |
|-----|--------|
| `enter` | confirm commit |
| `esc` | cancel |

## AI Commit Messages

When pressing `c`, differ uses `claude -p` (Claude CLI) to generate a commit message from the staged diff. The message is pre-filled in the input — edit or confirm with Enter.

Requires [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) installed. Falls back to empty input if unavailable.

## Themes

```bash
differ --theme dark   # default
differ --theme light
```

Config file: `~/.config/differ/config.json`

```json
{
  "theme": "dark",
  "commit_msg_cmd": "claude -p",
  "commit_msg_prompt": "Write a concise git commit message for this diff:"
}
```

## Features

- Syntax highlighting via Chroma (Go, JS/TS, Python, Rust, CSS, HTML, JSON, YAML, Markdown, ...)
- Staged/unstaged/untracked file indicators
- Stage/unstage individual files or all at once
- Commit flow with AI-generated messages
- Commit log browser with diff preview
- Compare against any branch/tag/commit ref
- Single binary, no runtime dependencies
