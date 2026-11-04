package generate

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"awesome-release-notes/internal/cliutil"
	"awesome-release-notes/internal/notes"

	"github.com/spf13/cobra"
)

var (
	gen    notes.Generator
	format string
)

var Cmd = &cobra.Command{
	Use:   "generate [flags] <output-file>",
	Short: "Generate Markdown release notes from Git commits",
	Long:  "Generate Markdown release notes from Git commits through the ending revision (HEAD by default).\nWithout reachable tags, the entire history is included. No Git executable is required.",
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return cliutil.NewArgumentError(errors.New("exactly one output file path is required"))
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if format != "markdown" {
			return cliutil.NewArgumentError(fmt.Errorf("unsupported format %q; supported formats: markdown", format))
		}
		if gen.Title != "" && (strings.TrimSpace(gen.Title) == "" || strings.ContainsAny(gen.Title, "\r\n")) {
			return cliutil.NewArgumentError(errors.New("--title must be a non-empty single line"))
		}
		gen.RepoPath, _ = cmd.Flags().GetString("repo")
		rendered, err := gen.Generate()
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[0], rendered, 0o644); err != nil {
			return fmt.Errorf("write release notes: %w", err)
		}
		log.Printf("[ok] Release notes generated successfully: %q", args[0])
		return nil
	},
}

func init() {
	flags := Cmd.Flags()
	flags.StringVar(&gen.ConfigPath, "config", "", cliutil.ConfigUsage)
	flags.StringVar(&gen.From, "from", "", "starting revision, excluded: tag, commit hash, or expression, not a branch (default: nearest tag reachable from the end commit)")
	flags.StringVar(&gen.To, "to", "", "ending revision, included: commit hash, branch, or tag (default: HEAD)")
	flags.StringVar(&format, "format", "markdown", "output format (markdown)")
	flags.StringVar(&gen.Title, "title", "", "release title, a non-empty single line (default: no title heading)")
}
