# differ

Terminal UI git diff viewer built with Go and Bubble Tea. Two-panel layout: file list + syntax-highlighted diff preview.

<p align="center">
  <img src="./assets/preview.png" alt="differ preview" width="800" />
</p>

## Install

```bash
brew install jansmrcka/tap/differ
```

Or via Go:

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

| Key           | Action                                     |
| ------------- | ------------------------------------------ |
| `j/k`         | navigate files                             |
| `enter` / `l` | view diff                                  |
| `tab`         | stage/unstage file                         |
| `a`           | stage all                                  |
| `r`           | enter review mode                          |
| `c`           | commit (AI-generated message via `claude`) |
| `b`           | open branch picker                         |
| `v`           | toggle split (side-by-side) diff           |
| `e`           | open in editor (`$EDITOR`, configurable)   |
| `P`           | push (auto `--set-upstream` if needed)     |
| `F`           | pull (fast-forward only)                   |
| `g/G`         | first/last file                            |
| `q`           | quit                                       |

### Diff View

| Key         | Action                    |
| ----------- | ------------------------- |
| `j/k`       | move line cursor          |
| `}` / `{`   | next/prev hunk            |
| `d/u`       | half page down/up         |
| `g/G`       | first/last line           |
| `n/p`       | next/prev file            |
| `tab`       | stage/unstage             |
| `b`         | open branch picker        |
| `v`         | toggle split diff         |
| `e`         | open in editor            |
| `esc` / `h` | back to file list         |

The diff view has a line cursor (`▌`) marking the current line. It is the
anchor review comments attach to, and it keeps the same position when you
toggle between unified and split view.

### Review Mode

Review mode is the diff view with review state on top: it tracks which files
you have looked at and (from the next release) holds your review comments.
It never changes git state — staging and committing stay explicit actions.

| Key       | Action                          |
| --------- | ------------------------------- |
| `r`       | toggle review mode              |
| `j/k`     | move line cursor                |
| `}` / `{` | next/prev hunk                  |
| `c`       | comment on line (edit existing) |
| `C`       | comment on whole hunk           |
| `x`       | delete comment under cursor     |
| `s`       | send comment under cursor       |
| `S`       | send all pending comments       |

Quitting with unsent comments asks for confirmation — review state is
session-only, so `q` really does discard them.
| `n/p`     | next/prev file                  |
| `v`       | toggle split diff               |
| `esc`     | back to file list               |

In the comment editor: `ctrl+s` saves, `esc` cancels. Comments are multiline,
shown inline under the line they refer to, and marked `pending` until sent.
They live for the session only — nothing is written to disk or to git.

### Commit Mode

| Key     | Action         |
| ------- | -------------- |
| `enter` | confirm commit |
| `esc`   | cancel         |

### Branch Picker

| Key             | Action               |
| --------------- | -------------------- |
| type            | filter branches      |
| `↑/↓` / `^j/^k` | navigate             |
| `enter`         | switch branch        |
| `ctrl+n`        | create new branch    |
| `esc`           | clear filter / close |

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
  "commit_msg_prompt": "Write a concise git commit message for this diff:",
  "editor_cmd": "tmux new-window -c {repo} nvim {file}",
  "split_diff": false,
  "feedback_target": "clipboard",
  "tmux_target": ""
}
```

`feedback_target` decides where review feedback goes: `clipboard` (default),
`stdout`, or `tmux`. The clipboard target shells out to `pbcopy` on macOS and
`wl-copy`/`xclip`/`xsel` on Linux, so it does the right thing over SSH.

A failed send never discards comments — they stay pending and the error is
shown, so you can fix the target and send again.

## Reviewing agent changes in tmux

The workflow differ is built for: a coding agent in one pane, differ in another.

```text
tmux
├── Claude Code
└── differ
```

```json
{
  "feedback_target": "tmux",
  "tmux_target": ""
}
```

An empty `tmux_target` means the last active pane — in a two-pane layout, and
from a `display-popup`, that is the pane you came from. Set it explicitly to
any tmux pane target (`%12`, `session:window.pane`) to pin it.

Press `r` to review, `c` to comment on a line, `S` to send everything pending.
The payload is pasted into the target pane's prompt using a tmux paste buffer,
so multiline feedback arrives intact.

differ does **not** press Enter for you. The feedback lands in the agent's
prompt and you send it — which means a misconfigured target can never execute
anything. differ also refuses to paste into its own pane.

If tmux is unavailable, the pane is gone, or the target resolves to differ
itself, the send fails with a specific error and your comments stay pending.

`editor_cmd` supports `{file}` (absolute path) and `{repo}` (repo root) placeholders. Defaults to `$EDITOR {file}` (falls back to `vi`).

## Tips

### Tmux floating window

Bind differ to a key in tmux for quick access as a popup overlay:

```tmux
bind g display-popup -E -w 90% -h 90% "cd #{pane_current_path} && differ"
```

Press `prefix + g` to open differ in a floating window over your current session. It closes automatically on quit.

## Features

- Syntax highlighting via Chroma (Go, JS/TS, Python, Rust, CSS, HTML, JSON, YAML, Markdown, ...)
- Staged/unstaged/untracked file indicators
- Stage/unstage individual files or all at once
- Split (side-by-side) diff view
- Branch picker with type-to-filter and branch creation (`ctrl+n`)
- Push with auto `--set-upstream` for new branches
- Pull with upstream ahead/behind tracking
- Per-file added/deleted line counts in file list
- Configurable editor command (`editor_cmd`)
- Commit flow with AI-generated messages
- Commit log browser with diff preview
- Compare against any branch/tag/commit ref
- Auto-refresh (2s polling)
- Single binary, no runtime dependencies
