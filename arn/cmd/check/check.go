package check

import (
	"github.com/steromano87/awesome-release-notes/internal/cliutil"
	"github.com/steromano87/awesome-release-notes/internal/notes"

	"github.com/spf13/cobra"
)

var configPath string

var Cmd = &cobra.Command{
	Use:   "check",
	Short: "Validate the configuration without generating release notes",
	Args:  cliutil.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		if err := (notes.Generator{RepoPath: repo, ConfigPath: configPath}).Check(); err != nil {
			return err
		}
		cmd.Println("[arn] [ok] Configuration is valid")
		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&configPath, "config", "", cliutil.ConfigUsage)
}
