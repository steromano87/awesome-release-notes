package testutil

import (
	"bytes"
	"fmt"

	"github.com/steromano87/awesome-release-notes/internal/cliutil"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Run executes root with args from a clean flag state, since commands and flags are package-level.
// It returns the exit status the CLI would use and prints the error like Execute does.
func Run(root *cobra.Command, args []string, stdout, stderr *bytes.Buffer) int {
	resetFlags(root)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
	}
	return cliutil.ExitCode(err)
}

func resetFlags(cmd *cobra.Command) {
	for _, flags := range []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags()} {
		flags.VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	for _, child := range cmd.Commands() {
		resetFlags(child)
	}
}
