package init

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steromano87/awesome-release-notes/internal/cliutil"
	"github.com/steromano87/awesome-release-notes/internal/config"
	"github.com/steromano87/awesome-release-notes/internal/gitlog"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Create the default " + config.DefaultName + ".yaml in the repository root",
	Args:  cliutil.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repoPath, _ := cmd.Flags().GetString("repo")
		repo, err := gitlog.Open(repoPath)
		if err != nil {
			return err
		}
		root := repo.Root
		if err := repo.Close(); err != nil {
			return err
		}
		path := filepath.Join(root, config.DefaultName+".yaml")
		// O_EXCL refuses to overwrite an existing configuration.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("configuration %q already exists", path)
		}
		if err != nil {
			return err
		}
		_, err = file.Write(config.DefaultYAML)
		if err = errors.Join(err, file.Close()); err != nil {
			return fmt.Errorf("write configuration: %w", err)
		}
		cmd.Printf("Created %s\n", path)
		return nil
	},
}
