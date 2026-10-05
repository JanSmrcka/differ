# Contributing to differ

Thanks for helping. Every kind of contribution is welcome — an issue with a
screenshot counts as much as a pull request.

## From fork to pull request

You need Go (the version in `go.mod`) and git.

```bash
# 1. Fork and clone (or use the Fork button on GitHub)
gh repo fork JanSmrcka/differ --clone && cd differ

# 2. Branch
git checkout -b fix/short-description

# 3. Build and try it in any git repository with changes
make build && ./bin/differ

# 4. Test and lint before you push
make test
GOTOOLCHAIN=go1.25.5 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1 run ./...

# 5. Commit, push, open the PR against master
git commit -am "fix: what changed, in a few words"
git push -u origin fix/short-description
gh pr create --fill
```

Write `Closes #123` in the PR description if it fixes an issue. CI runs the
same tests and lint; a review follows.

## A few rules

- **Test first.** A bug fix starts with a test that fails; a feature starts
  with a test that describes it.
- **A new key?** Add it to the table in `internal/ui/keymap.go` and to the
  README. A test fails if the two disagree.
- **Git only through `os/exec`**, styles only in `internal/ui/styles.go`, and
  no new dependencies without a good reason.
- **Small PRs** are reviewed fastest. One change per PR.

## Where things are

| | |
|---|---|
| `internal/git` | every git call |
| `internal/review` | the review session, its comments and its persistence |
| `internal/feedback` | delivering a review: clipboard, stdout, tmux, herdr |
| `internal/editor` | opening a file in an editor |
| `internal/ui` | the Bubble Tea models, the diff parser and renderer |

[`CLAUDE.md`](./CLAUDE.md) explains the decisions that are not obvious from
the code, including a long list of things that looked right and were not.
It is written for AI agents, and it is just as useful for people.

## Not sure?

Open an issue and ask. There are no wrong questions.
