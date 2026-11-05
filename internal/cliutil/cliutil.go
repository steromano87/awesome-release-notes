// Package cliutil holds what the command packages share without importing each other.
package cliutil

import (
	"errors"
	"fmt"

	"github.com/steromano87/awesome-release-notes/internal/config"

	"github.com/spf13/cobra"
)

// ArgumentError marks invalid usage; the CLI exits with status 2 for it.
type ArgumentError struct{ error }

func NewArgumentError(err error) error { return ArgumentError{err} }

// ExitCode maps a command error to the process exit status.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if _, ok := errors.AsType[ArgumentError](err); ok {
		return 2
	}
	return 1
}

const ConfigUsage = "JSON, YAML or TOML configuration (default: repository-root " + config.DefaultName + ".<extension>)"

func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return ArgumentError{fmt.Errorf("unexpected argument %q for %q", args[0], cmd.CommandPath())}
	}
	return nil
}
