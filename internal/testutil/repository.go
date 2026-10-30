package testutil

import (
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func Repository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false, git.WithDefaultBranch(plumbing.NewBranchReferenceName("main")))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commit.GpgSign = config.NewOptBool(false)
	cfg.Tag.GpgSign = config.NewOptBool(false)
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func Open(t *testing.T, dir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Error(err)
		}
	})
	return repo
}

func Commit(t *testing.T, dir, message string, parents ...string) string {
	t.Helper()
	repo := Open(t, dir)
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	var hashes []plumbing.Hash
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if len(parents) == 0 {
		head, err := repo.Head()
		if err == nil {
			parents = []string{head.Hash().String()}
		} else if err != plumbing.ErrReferenceNotFound {
			t.Fatal(err)
		}
	}
	for _, parent := range parents {
		hash := plumbing.NewHash(parent)
		commit, err := repo.CommitObject(hash)
		if err != nil {
			t.Fatal(err)
		}
		if next := commit.Committer.When.Add(time.Second); next.After(date) {
			date = next
		}
		hashes = append(hashes, hash)
	}
	hash, err := worktree.Commit(message, &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            signature(date),
		Parents:           hashes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}

func Tag(t *testing.T, dir, name string, annotated bool) {
	t.Helper()
	repo := Open(t, dir)
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	var options *git.CreateTagOptions
	if annotated {
		options = &git.CreateTagOptions{
			Message: name,
			Tagger:  signature(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)),
		}
	}
	if _, err := repo.CreateTag(name, head.Hash(), options); err != nil {
		t.Fatal(err)
	}
}

func Checkout(t *testing.T, dir, branch string, create bool) {
	t.Helper()
	repo := Open(t, dir)
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch), Create: create,
	}); err != nil {
		t.Fatal(err)
	}
}

func signature(date time.Time) *object.Signature {
	return &object.Signature{Name: "Release Notes Test", Email: "test@example.invalid", When: date}
}
