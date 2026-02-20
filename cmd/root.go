package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
	"github.com/jansmrcka/differ/internal/ui"
	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

var (
	flagStaged bool
	flagRef    string
	flagTheme  string
	flagCommit bool
)

var rootCmd = &cobra.Command{
	Use:     "differ",
	Short:   "Git diff TUI viewer",
	Version: version,
	RunE:    runDiff,
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Browse recent commits with diff preview",
	RunE:  runLog,
}

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Review staged changes and commit",
	RunE:  runCommit,
}

func init() {
	rootCmd.Flags().BoolVarP(&flagStaged, "staged", "s", false, "show only staged changes")
	rootCmd.Flags().StringVarP(&flagRef, "ref", "r", "", "compare against branch/tag/commit")
	rootCmd.Flags().BoolVarP(&flagCommit, "commit", "c", false, "enter commit mode after review")
	rootCmd.Flags().StringVar(&flagTheme, "theme", "", "color theme (dark, light)")
	rootCmd.AddCommand(logCmd, commitCmd)
}

// Execute runs the root CLI command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveTheme(cfg config.Config) theme.Theme {
	name := cfg.Theme
	if flagTheme != "" {
		name = flagTheme
	}
	if t, ok := theme.Themes[name]; ok {
		return t
	}
	return theme.DarkTheme()
}

func runDiff(cmd *cobra.Command, args []string) error {
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

	cfg := config.Load()
	t := resolveTheme(cfg)
	styles := ui.NewStyles(t)

	model := ui.NewModel(repo, cfg, files, untracked, styles, t, flagStaged, flagRef)
	if flagCommit {
		model.StartInCommitMode()
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := finalModel.(ui.Model); ok && m.SelectedFile != "" {
		return openInTmux(m.SelectedFile, repo.Dir())
	}
	return nil
}

func openInTmux(file, repoRoot string) error {
	absPath := filepath.Join(repoRoot, file)
	cmd := exec.Command("tmux", "new-window", "-c", repoRoot, "nvim", absPath)
	return cmd.Run()
}

func runCommit(cmd *cobra.Command, args []string) error {
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

	cfg := config.Load()
	t := resolveTheme(cfg)
	styles := ui.NewStyles(t)

	model := ui.NewModel(repo, cfg, files, nil, styles, t, true, "")
	model.StartInCommitMode()
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func runLog(cmd *cobra.Command, args []string) error {
	repo, err := git.NewRepo(".")
	if err != nil {
		return err
	}
	if !repo.HasCommits() {
		fmt.Println("No commits yet.")
		return nil
	}

	cfg := config.Load()
	t := resolveTheme(cfg)
	styles := ui.NewStyles(t)

	model := ui.NewLogModel(repo, styles, t)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
