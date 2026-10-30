package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"awesome-release-notes/internal/config"
	"awesome-release-notes/internal/gitlog"
	"awesome-release-notes/internal/notes"

	"github.com/spf13/cobra"
)

type argumentError struct{ error }

func Run(args []string, stdout, stderr io.Writer, version string) int {
	var repoPath, configPath, from, format, output, title string
	command := &cobra.Command{
		Use:           "arn-gen",
		Short:         "Generate Markdown release notes from Git commits",
		Long:          "Generate Markdown release notes from Git commits through HEAD.\nWithout reachable tags, the entire history is included. No Git executable is required.",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return argumentError{errors.New("positional arguments are not supported")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "markdown" {
				return argumentError{fmt.Errorf("unsupported format %q; supported formats: markdown", format)}
			}
			if strings.TrimSpace(title) == "" || strings.ContainsAny(title, "\r\n") {
				return argumentError{errors.New("--title must be a non-empty single line")}
			}
			return generate(repoPath, configPath, from, output, title, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	flags := command.Flags()
	flags.StringVar(&repoPath, "repo", ".", "Git repository or a directory inside it")
	flags.StringVar(&configPath, "config", "", "JSON, YAML or TOML configuration (default: repository-root "+config.DefaultName+".<extension>)")
	flags.StringVar(&from, "from", "", "starting tag, excluded (default: nearest tag reachable from HEAD)")
	flags.StringVar(&format, "format", "markdown", "output format (markdown)")
	flags.StringVar(&output, "output", "", "output file (default: stdout)")
	flags.StringVar(&title, "title", "Release notes", "release title (non-empty single line)")
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return argumentError{err} })
	command.SetVersionTemplate("{{.Version}}\n")
	command.SetArgs(append([]string{}, args...))
	command.SetOut(stdout)
	command.SetErr(stderr)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		var invalid argumentError
		if errors.As(err, &invalid) {
			return 2
		}
		return 1
	}
	return 0
}

func generate(repoPath, configPath, from, output, title string, stdout, stderr io.Writer) (err error) {
	logger := log.New(stderr, "[arn-gen] ", 0)
	repo, err := gitlog.Open(repoPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, repo.Close())
		if err == nil {
			if output == "" {
				logger.Print("[ok] Release notes generated successfully (stdout)")
			} else {
				logger.Printf("[ok] Release notes generated successfully: %q", output)
			}
		}
	}()
	logger.Printf("[info] Repository: %q", repo.Root)
	cfg, err := config.LoadFile(repo.Root, configPath)
	if err != nil {
		return err
	}
	commits, err := repo.Commits(from, logger)
	if err != nil {
		return err
	}
	logger.Printf("[info] Generating release notes from %d commits", len(commits))
	rendered, err := notes.Render(cfg, title, commits, logger)
	if err != nil {
		return err
	}
	if output != "" {
		if err := writeOutput(output, bytes.NewReader(rendered)); err != nil {
			return fmt.Errorf("write release notes: %w", err)
		}
		return nil
	}
	if _, err := stdout.Write(rendered); err != nil {
		return fmt.Errorf("write release notes: %w", err)
	}
	return nil
}
