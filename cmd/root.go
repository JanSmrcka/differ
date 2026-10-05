package cmd

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
	"github.com/jansmrcka/differ/internal/ui"
	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

// usageError marks a failure that is the command line's fault rather than the
// repository's, so Execute can exit 2 for it and 1 for everything else.
//
// It carries the command that failed: a bad flag on `differ log` has to print
// log's usage, not the root's, which lists flags log does not accept.
type usageError struct {
	cmd *cobra.Command
	err error
}

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

const (
	exitRuntime = 1
	exitUsage   = 2
)

var (
	flagStaged  bool
	flagRef     string
	flagTheme   string
	flagCommit  bool
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "differ",
	Short: "Review git changes in the terminal",
	Long: `differ shows what changed and lets you review it.

Run it with no arguments to see the working tree; add -s for the index, or
-r <ref> to compare against a branch, tag or commit. In the diff you can
comment line by line and send the result to a coding agent; "differ review"
opens the same changes straight into the first diff.`,
	Example: `  differ                 # everything that changed
  differ -s              # staged changes only
  differ -r main         # compare against main
  differ -c              # open straight into the commit message
  differ review          # review the working tree, ready to comment
  differ review -s       # review the staged changes
  differ log             # browse recent commits
  differ commit          # review what is staged, then commit`,
	// Version is assigned in init, not here: this literal is evaluated when
	// the package variable is initialised, which is before init runs — so the
	// build-info fallback below never reached Cobra and `go install …@latest`
	// reported "dev" forever.
	RunE: runDiff,
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review changes and comment on them line by line",
	Long: `review opens the same changes as differ itself, but starts in the first diff.

Comment on a line with c, or on a whole hunk with C, then send one comment
with s or all of them with S. Where the feedback goes is set by
feedback_target in the config: the clipboard, stdout, or a tmux pane running
an agent.`,
	Example: `  differ review          # the working tree
  differ review -s       # the staged changes
  differ review -r main  # everything that differs from main`,
	RunE: runReview,
}

var logCmd = &cobra.Command{
	Use:     "log",
	Short:   "Browse recent commits with a diff preview",
	Example: "  differ log",
	RunE:    runLog,
}

var commitCmd = &cobra.Command{
	Use:     "commit",
	Short:   "Review what is staged, then commit it",
	Long:    "commit shows the staged changes and opens the commit message input, with an AI-generated message if commit_msg_cmd is configured.",
	Example: "  differ commit",
	RunE:    runCommit,
}

func init() {
	info, ok := debug.ReadBuildInfo()
	setVersion(resolveVersion(version, info, ok))

	// Usage belongs to a bad command line, not to a repository that turned
	// out to have no such ref: printing it over a runtime failure buries the
	// one line that says what happened. Cobra is told to stay quiet and
	// Execute reports the error itself.
	for _, c := range []*cobra.Command{rootCmd, reviewCmd, logCmd, commitCmd} {
		c.SilenceUsage = true
		c.SilenceErrors = true
	}

	// The comparison flags mean the same thing wherever they appear.
	for _, c := range []*cobra.Command{rootCmd, reviewCmd} {
		c.Flags().BoolVarP(&flagStaged, "staged", "s", false, "only what is staged")
		c.Flags().StringVarP(&flagRef, "ref", "r", "", "compare against a branch, tag or commit")
	}
	for _, c := range []*cobra.Command{rootCmd, reviewCmd, logCmd, commitCmd} {
		c.Flags().StringVar(&flagTheme, "theme", "",
			"colour theme: "+strings.Join(theme.ThemeNames(), ", "))
		c.Flags().BoolVar(&flagNoColor, "no-color", false, "disable colour (also honours NO_COLOR)")
	}
	rootCmd.Flags().BoolVarP(&flagCommit, "commit", "c", false, "open straight into the commit message")

	rootCmd.AddCommand(reviewCmd, logCmd, commitCmd)
}

// Execute runs the CLI and turns a failure into a concise line and an exit
// code: 0 success, 1 a runtime problem, 2 a bad command line.
func Execute() {
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usageError{cmd: c, err: err}
	})

	err := rootCmd.Execute()
	if err == nil {
		return
	}

	var ue usageError
	switch {
	case errors.As(err, &ue):
		reportUsage(ue.cmd, ue.err)
	case isUnknownCommand(err):
		// Cobra reports this itself rather than through FlagErrorFunc, but a
		// command that does not exist is still a bad command line, not a
		// problem with the repository.
		reportUsage(rootCmd, err)
	default:
		fmt.Fprintf(os.Stderr, "differ: %v\n", err)
		os.Exit(exitRuntime)
	}
}

// reportUsage prints the concise line, then the usage of the command that
// actually failed.
func reportUsage(c *cobra.Command, err error) {
	if c == nil {
		c = rootCmd
	}
	fmt.Fprintf(os.Stderr, "differ: %v\n\n", err)
	c.SetOut(os.Stderr)
	_ = c.Usage()
	os.Exit(exitUsage)
}

// isUnknownCommand spots cobra's own wording for a command it does not have.
// There is no sentinel error to match on, which is why this reads the text.
func isUnknownCommand(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag")
}

// resolveTheme picks the theme, and refuses a name it does not have.
//
// It used to fall back to dark without a word, so a typo in --theme left the
// user wondering why the colours had not changed. A name from the *config*
// still falls back rather than refusing: a stale config file should not stop
// differ from opening.
func resolveTheme(cfg config.Config) (theme.Theme, error) {
	// The name is checked before the no-colour short-circuit. Accepting a
	// typo because NO_COLOR happened to be set would make whether differ
	// reports the mistake depend on an unrelated environment variable.
	named, ok := theme.Themes[flagTheme]
	if flagTheme != "" && !ok {
		return theme.Theme{}, fmt.Errorf("unknown theme %q — use one of: %s",
			flagTheme, strings.Join(theme.ThemeNames(), ", "))
	}

	// NO_COLOR is a convention worth honouring: set and not empty means no
	// colour. https://no-color.org — "present and not an empty string".
	if flagNoColor || os.Getenv("NO_COLOR") != "" {
		return theme.NoColorTheme(), nil
	}
	if flagTheme != "" {
		return named, nil
	}
	if t, ok := theme.Themes[cfg.Theme]; ok {
		return t, nil
	}
	return theme.DarkTheme(), nil
}

func runDiff(cmd *cobra.Command, args []string) error { return openDiff(cmd, false) }

func runReview(cmd *cobra.Command, args []string) error { return openDiff(cmd, true) }

// openDiff builds the model for the current changeset and runs the TUI.
func openDiff(cmd *cobra.Command, review bool) error {
	// The command line is checked before the repository. A typo in --theme is
	// the user's mistake either way, and reporting it should not depend on
	// where they happened to run differ from.
	cfg := config.Load()
	t, err := resolveTheme(cfg)
	if err != nil {
		return usageError{cmd: cmd, err: err}
	}

	repo, err := git.NewRepo(".")
	if err != nil {
		return err
	}

	files, err := repo.ChangedFiles(flagStaged, flagRef)
	if err != nil {
		return err
	}

	var untracked []string
	if !flagStaged && flagRef == "" {
		untracked, err = repo.UntrackedFiles()
		if err != nil {
			return err
		}
	}

	model := ui.NewModel(repo, cfg, files, untracked, ui.NewStyles(t), t, flagStaged, flagRef)
	switch {
	case review:
		model.StartInReviewMode()
	case flagCommit:
		model.StartInCommitMode()
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if m, ok := finalModel.(ui.Model); ok {
		// Before the error check: the claim on the review file has to be
		// given up however the program ended, or the next differ in this
		// repository would find it held and refuse to save.
		defer m.Close()
	}
	if err != nil {
		return err
	}
	if m, ok := finalModel.(ui.Model); ok {
		// The alt screen is gone now, so buffered feedback can be printed.
		if err := m.FlushFeedback(os.Stdout); err != nil {
			return err
		}
	}
	return nil
}
func runCommit(cmd *cobra.Command, args []string) error {
	cfg := config.Load()
	t, err := resolveTheme(cfg)
	if err != nil {
		return usageError{cmd: cmd, err: err}
	}

	repo, err := git.NewRepo(".")
	if err != nil {
		return err
	}

	files, err := repo.ChangedFiles(true, "")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("No staged changes to commit.")
		return nil
	}

	styles := ui.NewStyles(t)

	model := ui.NewModel(repo, cfg, files, nil, styles, t, true, "")
	model.StartInCommitMode()
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if m, ok := finalModel.(ui.Model); ok {
		defer m.Close()
	}
	if err != nil {
		return err
	}
	if m, ok := finalModel.(ui.Model); ok {
		return m.FlushFeedback(os.Stdout)
	}
	return nil
}

func runLog(cmd *cobra.Command, args []string) error {
	cfg := config.Load()
	t, err := resolveTheme(cfg)
	if err != nil {
		return usageError{cmd: cmd, err: err}
	}

	repo, err := git.NewRepo(".")
	if err != nil {
		return err
	}
	if !repo.HasCommits() {
		fmt.Println("No commits yet.")
		return nil
	}

	styles := ui.NewStyles(t)

	model := ui.NewLogModel(repo, styles, t, cfg.TabWidth)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// resolveVersion picks what --version reports.
//
// ldflags win: a release build is told exactly what it is. Otherwise the
// module version the binary was built from, which is what `go install
// github.com/jansmrcka/differ@latest` leaves behind. "(devel)" means a local
// build of an untagged tree, which is less informative than "dev".
func resolveVersion(ldflags string, info *debug.BuildInfo, ok bool) string {
	if ldflags != "dev" {
		return ldflags
	}
	if ok && info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return ldflags
}

// setVersion keeps the package variable and what Cobra prints in step. They
// were set in two places, one of which ran first and won.
func setVersion(v string) {
	version = v
	rootCmd.Version = v
}
