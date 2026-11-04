package notes

import (
	"errors"
	"log"

	"awesome-release-notes/internal/config"
	"awesome-release-notes/internal/gitlog"
)

// Generator builds the release notes of a Git repository. Empty fields use the defaults of the CLI.
type Generator struct {
	RepoPath   string // Git repository or a directory inside it; "." when empty
	ConfigPath string // configuration file; the repository-root default when empty
	From       string // excluded starting revision; nearest reachable tag when empty
	To         string // included ending revision; HEAD when empty
	Title      string
}

func (g Generator) Generate() (notes []byte, err error) {
	repo, cfg, err := g.open()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, repo.Close()) }()
	commits, err := repo.Commits(g.From, g.To)
	if err != nil {
		return nil, err
	}
	log.Printf("[info] Generating release notes from %d commits", len(commits))
	return NewRenderer(cfg).Render(g.Title, commits)
}

// Check loads and validates the configuration without reading any commit.
func (g Generator) Check() error {
	repo, _, err := g.open()
	if err != nil {
		return err
	}
	return repo.Close()
}

// open returns the repository and its validated configuration; the caller closes the repository.
func (g Generator) open() (*gitlog.Repository, *config.Config, error) {
	repoPath := g.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	repo, err := gitlog.Open(repoPath)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("[info] Repository: %q", repo.Root)
	cfg, err := config.LoadFile(repo.Root, g.ConfigPath)
	if err != nil {
		return nil, nil, errors.Join(err, repo.Close())
	}
	return repo, cfg, nil
}
