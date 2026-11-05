package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/steromano87/awesome-release-notes/arn/cmd/check"
	"github.com/steromano87/awesome-release-notes/arn/cmd/generate"
	initcmd "github.com/steromano87/awesome-release-notes/arn/cmd/init"
	"github.com/steromano87/awesome-release-notes/internal/cliutil"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X github.com/steromano87/awesome-release-notes/arn/cmd.version=...".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "arn",
	Short:         "Generate Markdown release notes from Git commits",
	Version:       version,
	SilenceErrors: true,
	SilenceUsage:  true,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

// Execute executes the root command and exits with a non-zero status on failure.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(cliutil.ExitCode(err))
	}
}

func init() {
	log.SetOutput(writerFunc(func(p []byte) (int, error) { return rootCmd.OutOrStdout().Write(p) }))
	log.SetPrefix("[arn] ")
	log.SetFlags(0)
	rootCmd.PersistentFlags().String("repo", ".", "Git repository or a directory inside it")
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return cliutil.NewArgumentError(err) })
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(generate.Cmd, check.Cmd, initcmd.Cmd)
}

// writerFunc lets the logger follow the output stream of the running command.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
