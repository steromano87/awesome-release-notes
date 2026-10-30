package gitlog

import (
	"fmt"
	"testing"
	"time"

	"awesome-release-notes/internal/testutil"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestNearestTagTieBreaks(t *testing.T) {
	for _, sameCommit := range []bool{false, true} {
		for _, tt := range []struct {
			name                  string
			first, second         string
			firstDate, secondDate int64
			firstAnnotated        bool
			secondAnnotated       bool
			want                  string
		}{
			{"annotation", "a-lightweight", "z-annotated", 0, 1, false, true, "z-annotated"},
			{"date", "a-old", "z-new", 1, 2, true, true, "z-new"},
			{"annotated name", "z-tag", "a-tag", 1, 1, true, true, "a-tag"},
			{"lightweight name", "z-tag", "a-tag", 0, 0, false, false, "a-tag"},
		} {
			t.Run(fmt.Sprintf("%s_same_commit_%t", tt.name, sameCommit), func(t *testing.T) {
				dir := testutil.Repository(t)
				root := testutil.Commit(t, dir, "root")
				left := testutil.Commit(t, dir, "left", root)
				right := testutil.Commit(t, dir, "right", root)
				head := testutil.Commit(t, dir, "merge", left, right)
				repo := open(t, dir)
				graph := make(map[plumbing.Hash]*object.Commit)
				for _, hash := range []string{root, left, right, head} {
					commit, err := repo.repo.CommitObject(plumbing.NewHash(hash))
					if err != nil {
						t.Fatal(err)
					}
					graph[commit.Hash] = commit
				}
				secondHash := right
				if sameCommit {
					secondHash = left
				}
				for _, tag := range []struct {
					name, hash string
					annotated  bool
					date       int64
				}{
					{tt.first, left, tt.firstAnnotated, tt.firstDate},
					{tt.second, secondHash, tt.secondAnnotated, tt.secondDate},
				} {
					var options *git.CreateTagOptions
					if tag.annotated {
						options = &git.CreateTagOptions{
							Message: tag.name,
							Tagger: &object.Signature{
								Name: "Test", Email: "test@example.invalid", When: time.Unix(tag.date, 0),
							},
						}
					}
					if _, err := repo.repo.CreateTag(tag.name, plumbing.NewHash(tag.hash), options); err != nil {
						t.Fatal(err)
					}
				}
				hash, name, err := repo.nearestTag(graph)
				wantHash := left
				if tt.want == tt.second {
					wantHash = secondHash
				}
				if err != nil || name != tt.want || hash.String() != wantHash {
					t.Fatalf("selected %q (%s), want %q (%s): %v", name, hash, tt.want, wantHash, err)
				}
			})
		}
	}
}

func TestNearestTagManyOldTags(t *testing.T) {
	repo, graph, head := linearTaggedHistory(t, 2000, 1000)
	parent := graph[head].ParentHashes[0]
	if _, err := repo.repo.CreateTag("annotated-ancestor", parent, &git.CreateTagOptions{
		Message: "newer annotation on an older commit",
		Tagger: &object.Signature{
			Name: "Test", Email: "test@example.invalid", When: time.Unix(10000, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	hash, name, err := repo.nearestTag(graph)
	if err != nil || hash != head || name != "v002000" {
		t.Fatalf("selected %q (%s), want v002000 (%s): %v", name, hash, head, err)
	}
}

func BenchmarkNearestTag(b *testing.B) {
	for _, tags := range []int{1, 1000} {
		b.Run(fmt.Sprintf("50000_commits_%d_tags", tags), func(b *testing.B) {
			repo, graph, head := linearTaggedHistory(b, 50000, tags)
			b.ReportAllocs()
			for b.Loop() {
				hash, name, err := repo.nearestTag(graph)
				if err != nil || hash != head || name != "v050000" {
					b.Fatalf("selected %q (%s), want v050000 (%s): %v", name, hash, head, err)
				}
			}
		})
	}
}

func linearTaggedHistory(t testing.TB, count, tags int) (*Repository, map[plumbing.Hash]*object.Commit, plumbing.Hash) {
	t.Helper()
	storage := memory.NewStorage()
	gitRepo, err := git.Init(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gitRepo.Close(); err != nil {
			t.Error(err)
		}
	})
	graph := make(map[plumbing.Hash]*object.Commit, count)
	var head plumbing.Hash
	for i := 1; i <= count; i++ {
		signature := object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Unix(int64(i), 0)}
		commit := &object.Commit{Author: signature, Committer: signature, Message: "change"}
		if i > 1 {
			commit.ParentHashes = []plumbing.Hash{head}
		}
		encoded := storage.NewEncodedObject()
		if err := commit.Encode(encoded); err != nil {
			t.Fatal(err)
		}
		head, err = storage.SetEncodedObject(encoded)
		if err != nil {
			t.Fatal(err)
		}
		commit.Hash = head
		graph[head] = commit
		if i%(count/tags) == 0 {
			ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(fmt.Sprintf("v%06d", i)), head)
			if err := storage.SetReference(ref); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &Repository{repo: gitRepo}, graph, head
}
