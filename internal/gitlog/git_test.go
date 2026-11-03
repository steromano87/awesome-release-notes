package gitlog

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"awesome-release-notes/internal/testutil"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func TestCommitsWithoutTags(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := testutil.Repository(t)
	first := testutil.Commit(t, dir, "Initial commit")
	second := testutil.Commit(t, dir, "feat: support Unicode caf\u00e9\n\nBody with\nmultiple lines.")
	repo := open(t, dir)
	commits, err := repo.Commits("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Hash != first || commits[1].Hash != second {
		t.Fatalf("expected all commits oldest-first, got %+v", commits)
	}
	got := commits[1]
	if got.Subject != "feat: support Unicode caf\u00e9" || got.Body != "Body with\nmultiple lines." ||
		got.Message != got.Subject+"\n\n"+got.Body || got.Author != "Release Notes Test" || got.Date.IsZero() {
		t.Fatalf("unexpected commit data: %+v", got)
	}
	if commits[0].Body != "" || commits[0].Message != "Initial commit" {
		t.Fatalf("unexpected subject-only message: %+v", commits[0])
	}
}

func TestCommitsTo(t *testing.T) {
	dir := testutil.Repository(t)
	first := testutil.Commit(t, dir, "first")
	second := testutil.Commit(t, dir, "second")
	testutil.Commit(t, dir, "third")
	repo := open(t, dir)
	commits, err := repo.Commits("", second)
	if err != nil || len(commits) != 2 || commits[0].Hash != first || commits[1].Hash != second {
		t.Fatalf("expected first and second, got %+v, %v", commits, err)
	}
	if _, err := repo.Commits("", "missing"); err == nil {
		t.Fatal("expected error for unknown revision")
	}
}

func TestTagSelection(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1.0.0", false)
	second := testutil.Commit(t, dir, "feat: second")
	testutil.Tag(t, dir, "v2.0.0", true)
	third := testutil.Commit(t, dir, "fix: third")
	testutil.Checkout(t, dir, "unmerged", true)
	testutil.Commit(t, dir, "unmerged")
	testutil.Tag(t, dir, "v99.0.0", false)
	testutil.Checkout(t, dir, "main", false)
	repo := open(t, dir)
	for _, tt := range []struct {
		tag    string
		hashes []string
	}{
		{"", []string{third}},
		{"v1.0.0", []string{second, third}},
		{"v2.0.0", []string{third}},
		{second, []string{third}},
		{"v1.0.0^{commit}", []string{second, third}},
		{"HEAD~1", []string{third}},
	} {
		t.Run("from_"+tt.tag, func(t *testing.T) {
			commits, err := repo.Commits(tt.tag, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(commits) != len(tt.hashes) {
				t.Fatalf("expected %d commits, got %+v", len(tt.hashes), commits)
			}
			for i, hash := range tt.hashes {
				if commits[i].Hash != hash {
					t.Fatalf("commit %d: got %s, want %s", i, commits[i].Hash, hash)
				}
			}
		})
	}
	for _, tag := range []string{"missing", "v99.0.0", "--all", "v1.0.0..HEAD", "main", "unmerged"} {
		t.Run("reject_"+tag, func(t *testing.T) {
			if _, err := repo.Commits(tag, ""); err == nil {
				t.Fatalf("expected error for invalid, missing, or unreachable tag %q", tag)
			}
		})
	}
	testutil.Tag(t, dir, "v3.0.0", false)
	commits, err := repo.Commits("", "")
	if err != nil || len(commits) != 0 {
		t.Fatalf("HEAD at tag should yield no commits, got %+v, %v", commits, err)
	}
}

func TestMergeHistory(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1", false)
	testutil.Checkout(t, dir, "feature", true)
	feature := testutil.Commit(t, dir, "feat: feature branch")
	testutil.Checkout(t, dir, "main", false)
	main := testutil.Commit(t, dir, "fix: main branch")
	testutil.Commit(t, dir, "Merge feature", main, feature)
	repo := open(t, dir)
	commits, err := repo.Commits("", "")
	if err != nil {
		t.Fatal(err)
	}
	hashes := make(map[string]bool)
	for _, commit := range commits {
		hashes[commit.Hash] = true
	}
	if len(commits) != 3 || !hashes[feature] || !hashes[main] {
		t.Fatalf("expected commits from both parents and merge commit, got %+v", commits)
	}
}

func TestOpenAndEmptyRepository(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected error outside a Git repository")
	}
	dir := testutil.Repository(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Error(err)
		}
	})
	rootInfo, err := os.Stat(repo.Root)
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(rootInfo, dirInfo) {
		t.Fatalf("got root %q, want the same directory as %q", repo.Root, dir)
	}
	if _, err := repo.Commits("", ""); err == nil {
		t.Fatal("expected explicit error for a repository without commits")
	}
}

func TestNearestTagAcrossMerge(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Checkout(t, dir, "feature", true)
	var feature string
	for i := 0; i < 5; i++ {
		feature = testutil.Commit(t, dir, "feat: feature work")
	}
	testutil.Tag(t, dir, "v2", true)
	testutil.Checkout(t, dir, "main", false)
	main := testutil.Commit(t, dir, "fix: main branch")
	testutil.Tag(t, dir, "v999", false)
	merge := testutil.Commit(t, dir, "Merge feature", main, feature)
	repo := open(t, dir)
	for _, tag := range []string{"", "v2"} {
		commits, err := repo.Commits(tag, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(commits) != 2 || commits[0].Hash != main || commits[1].Hash != merge {
			t.Fatalf("from %q: expected only main-branch change and merge, got %+v", tag, commits)
		}
	}
}

func TestNestedAndNonCommitTags(t *testing.T) {
	dir := testutil.Repository(t)
	first := testutil.Commit(t, dir, "initial")
	storage := testutil.Open(t, dir)
	initial, err := storage.CommitObject(plumbing.NewHash(first))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateTag("tree", initial.TreeHash, nil); err != nil {
		t.Fatal(err)
	}
	repo := open(t, dir)
	commits, err := repo.Commits("", "")
	if err != nil || len(commits) != 1 {
		t.Fatalf("non-commit tag should not hide history: %+v, %v", commits, err)
	}
	if _, err := repo.Commits("tree", ""); err == nil {
		t.Fatalf("expected non-commit tag error, got %v", err)
	}
	testutil.Tag(t, dir, "inner", true)
	inner, err := storage.Tag("inner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateTag("outer", inner.Hash(), &git.CreateTagOptions{
		Message: "Nested annotation",
		Tagger: &object.Signature{
			Name: "Test", Email: "test@example.invalid", When: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatal(err)
	}
	second := testutil.Commit(t, dir, "feat: after nested tag")
	for _, tag := range []string{"", "outer"} {
		commits, err := repo.Commits(tag, "")
		if err != nil || len(commits) != 1 || commits[0].Hash != second {
			t.Fatalf("nested tag %q: got %+v, %v", tag, commits, err)
		}
	}
}

func TestDetachedHeadAndUnreachableTags(t *testing.T) {
	dir := testutil.Repository(t)
	first := testutil.Commit(t, dir, "initial")
	testutil.Commit(t, dir, "later")
	testutil.Tag(t, dir, "future", true)
	storage := testutil.Open(t, dir)
	worktree, err := storage.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(first)}); err != nil {
		t.Fatal(err)
	}
	commits, err := open(t, dir).Commits("", "")
	if err != nil || len(commits) != 1 || commits[0].Hash != first {
		t.Fatalf("expected detached HEAD history despite unreachable tag: %+v, %v", commits, err)
	}
}

func TestIncompleteHistory(t *testing.T) {
	dir := testutil.Repository(t)
	first := testutil.Commit(t, dir, "initial")
	testutil.Commit(t, dir, "later")
	storage := testutil.Open(t, dir)
	if err := storage.DeleteObject(plumbing.NewHash(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, dir).Commits("", ""); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("expected incomplete history error, got %v", err)
	}
}

func TestShallowHistory(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	head := testutil.Commit(t, dir, "later")
	storage := testutil.Open(t, dir)
	if err := storage.Storer.SetShallow([]plumbing.Hash{plumbing.NewHash(head)}); err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, dir).Commits("", ""); err == nil || !strings.Contains(err.Error(), "fetch complete history") {
		t.Fatalf("expected shallow history error, got %v", err)
	}
}

func TestAbbreviate(t *testing.T) {
	hashes := []string{
		"1234567abcdef0000000000000000000000000000",
		"1234567abcde00000000000000000000000000000",
		"abcdef01234560000000000000000000000000000",
	}
	slices.Sort(hashes)
	got := abbreviate(hashes)
	seen := make(map[string]bool)
	for _, hash := range hashes {
		short := got[hash]
		if len(short) < 7 || !strings.HasPrefix(hash, short) || seen[short] {
			t.Fatalf("invalid or duplicate abbreviation %q for %s", short, hash)
		}
		seen[short] = true
	}
	if got[hashes[2]] != hashes[2][:7] {
		t.Fatalf("non-colliding hash should use seven characters: %v", got)
	}
	if len(got[hashes[0]]) != 13 || len(got[hashes[1]]) != 13 {
		t.Fatalf("colliding hashes should extend just enough to be unique: %v", got)
	}
}

func TestSplitMessage(t *testing.T) {
	for _, tt := range []struct {
		message string
		subject string
		body    string
	}{
		{"subject\n", "subject", ""},
		{"first line\nsecond line\n\nbody\n\nmore\n", "first line second line", "body\n\nmore"},
		{"\nsubject\r\n\r\nbody\r\n", "subject", "body"},
		{" \n\nsubject\n\nbody", "subject", "body"},
		{"subject\n \n\nbody\n", "subject", "body"},
		{"", "", ""},
	} {
		subject, body := splitMessage(tt.message)
		if subject != tt.subject || body != tt.body {
			t.Errorf("%q: got subject %q, body %q; want %q, %q", tt.message, subject, body, tt.subject, tt.body)
		}
	}
}

func open(t *testing.T, dir string) *Repository {
	t.Helper()
	repo, err := Open(dir)
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
