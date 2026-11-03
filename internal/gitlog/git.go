package gitlog

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

type Commit struct {
	Hash      string
	ShortHash string
	Subject   string
	Body      string
	Message   string
	Author    string
	Date      time.Time
}

type Repository struct {
	Root string
	repo *git.Repository
}

func Open(path string) (*Repository, error) {
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open Git repository: %w", err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open Git worktree: %w", err), repo.Close())
	}
	root, err := filepath.Abs(worktree.Filesystem().Root())
	if err != nil {
		return nil, errors.Join(fmt.Errorf("resolve repository root: %w", err), repo.Close())
	}
	return &Repository{Root: root, repo: repo}, nil
}

func (r *Repository) Close() error {
	if err := r.repo.Close(); err != nil {
		return fmt.Errorf("close Git repository: %w", err)
	}
	return nil
}

// Commits returns oldest-first commits reachable from HEAD (or to) but not from the base tag.
// Without a tag, the reachable tag with the fewest commits ahead of it is selected.
// A non-empty to is any revision (hash, branch, tag, HEAD~1) replacing HEAD as the end of the range.
func (r *Repository) Commits(tag, to string) ([]Commit, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("repository must have at least one commit: %w", err)
	}
	end := head.Hash()
	if to != "" {
		end, err = r.resolveCommit(to)
		if err != nil {
			return nil, err
		}
		log.Printf("[info] Ending at commit: %s", end)
	} else if head.Name().IsBranch() {
		log.Printf("[info] Running on branch: %q", head.Name().Short())
	} else {
		log.Printf("[info] Running on detached HEAD: %s", head.Hash())
	}
	shallow, err := r.repo.Storer.Shallow()
	if err != nil {
		return nil, fmt.Errorf("read Git shallow boundaries: %w", err)
	}
	if len(shallow) > 0 {
		return nil, errors.New("shallow Git history is incomplete; fetch complete history and tags")
	}
	iter, err := r.repo.Log(&git.LogOptions{From: end, Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, fmt.Errorf("read Git history: %w", err)
	}
	defer iter.Close()
	var history []*object.Commit
	graph := make(map[plumbing.Hash]*object.Commit)
	if err := iter.ForEach(func(commit *object.Commit) error {
		history = append(history, commit)
		graph[commit.Hash] = commit
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read Git history (fetch complete history for shallow clones): %w", err)
	}
	for _, commit := range history {
		for _, parent := range commit.ParentHashes {
			if graph[parent] == nil {
				return nil, fmt.Errorf("incomplete Git history at %s; fetch complete history and tags", parent)
			}
		}
	}
	var base plumbing.Hash
	if tag != "" {
		if _, err := r.repo.Reference(plumbing.NewTagReferenceName(tag), true); err != nil {
			if _, err := r.repo.Reference(plumbing.NewBranchReferenceName(tag), true); err == nil {
				return nil, fmt.Errorf("--from %q is a branch; use a tag, commit hash, or revision", tag)
			}
		}
		base, err = r.resolveCommit(tag)
		if err != nil {
			return nil, err
		}
		if graph[base] == nil {
			return nil, fmt.Errorf("revision %q must be an ancestor of the end commit", tag)
		}
	} else {
		base, tag, err = r.nearestTag(graph)
		if err != nil {
			return nil, err
		}
	}
	if tag == "" {
		log.Print("[info] No reachable tag found; including all history from the first commit")
	} else {
		log.Printf("[info] Previous tag: %q (excluded from the commit range)", tag)
	}
	excluded := ancestors(graph, base)
	hashes := make([]string, 0, len(history))
	for _, commit := range history {
		hashes = append(hashes, commit.Hash.String())
	}
	slices.Sort(hashes)
	abbreviations := abbreviate(hashes)
	result := make([]Commit, 0, len(history)-len(excluded))
	for _, commit := range slices.Backward(history) {
		if excluded[commit.Hash] {
			continue
		}
		subject, body := splitMessage(commit.Message)
		message := subject
		if body != "" {
			message += "\n\n" + body
		}
		hash := commit.Hash.String()
		result = append(result, Commit{
			Hash: hash, ShortHash: abbreviations[hash],
			Subject: subject, Body: body, Message: message,
			Author: commit.Author.Name, Date: commit.Author.When,
		})
	}
	return result, nil
}

var errNotCommit = errors.New("tag does not point to a commit")

// resolveCommit resolves a hash, branch, tag, or revision expression to a commit.
func (r *Repository) resolveCommit(rev string) (plumbing.Hash, error) {
	// ResolveRevision cannot peel nested annotated tags, so exact tag names are peeled here.
	if ref, err := r.repo.Reference(plumbing.NewTagReferenceName(rev), true); err == nil {
		commit, _, _, err := r.peelTag(ref.Hash())
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("resolve tag %q: %w", rev, err)
		}
		return commit.Hash, nil
	}
	hash, err := r.repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve revision %q: %w", rev, err)
	}
	if _, err := r.repo.CommitObject(*hash); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("revision %q does not point to a commit: %w", rev, err)
	}
	return *hash, nil
}

func (r *Repository) peelTag(hash plumbing.Hash) (*object.Commit, bool, time.Time, error) {
	var annotated bool
	var date time.Time
	for {
		obj, err := r.repo.Object(plumbing.AnyObject, hash)
		if err != nil {
			return nil, false, time.Time{}, err
		}
		switch obj := obj.(type) {
		case *object.Commit:
			return obj, annotated, date, nil
		case *object.Tag:
			if !annotated {
				date = obj.Tagger.When
			}
			annotated = true
			hash = obj.Target
		default:
			return nil, false, time.Time{}, errNotCommit
		}
	}
}

func (r *Repository) nearestTag(graph map[plumbing.Hash]*object.Commit) (plumbing.Hash, string, error) {
	refs, err := r.repo.Tags()
	if err != nil {
		return plumbing.ZeroHash, "", fmt.Errorf("read Git tags: %w", err)
	}
	defer refs.Close()
	type candidate struct {
		name      string
		annotated bool
		date      time.Time
	}
	preferred := func(tag, best candidate) bool {
		return tag.annotated && !best.annotated ||
			tag.annotated == best.annotated && (tag.date.After(best.date) || tag.date.Equal(best.date) && tag.name < best.name)
	}
	candidates := make(map[plumbing.Hash]candidate)
	var parents []plumbing.Hash
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		commit, annotated, date, err := r.peelTag(ref.Hash())
		if errors.Is(err, errNotCommit) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tag %q: %w", ref.Name().Short(), err)
		}
		if graph[commit.Hash] == nil {
			return nil
		}
		tag := candidate{name: ref.Name().Short(), annotated: annotated, date: date}
		previous, exists := candidates[commit.Hash]
		if !exists {
			parents = append(parents, commit.ParentHashes...)
		}
		if !exists || preferred(tag, previous) {
			candidates[commit.Hash] = tag
		}
		return nil
	})
	if err != nil {
		return plumbing.ZeroHash, "", err
	}
	// A tagged ancestor always leaves more commits ahead than its tagged descendant.
	var excluded map[plumbing.Hash]bool
	if len(candidates) > 1 {
		excluded = ancestors(graph, parents...)
	}
	var best plumbing.Hash
	var bestTag candidate
	bestAhead := len(graph) + 1
	for hash, tag := range candidates {
		if excluded[hash] {
			continue
		}
		// shortcut: incomparable tagged tips need separate counts; revisit for slow branch-heavy histories.
		ahead := len(graph) - len(ancestors(graph, hash))
		if ahead < bestAhead || ahead == bestAhead && preferred(tag, bestTag) {
			best, bestAhead, bestTag = hash, ahead, tag
		}
	}
	return best, bestTag.name, nil
}

func ancestors(graph map[plumbing.Hash]*object.Commit, starts ...plumbing.Hash) map[plumbing.Hash]bool {
	seen := make(map[plumbing.Hash]bool)
	pending := slices.Clone(starts)
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[hash] || graph[hash] == nil {
			continue
		}
		seen[hash] = true
		pending = append(pending, graph[hash].ParentHashes...)
	}
	return seen
}

// Sorted neighboring hashes are enough to find a unique prefix for every commit.
func abbreviate(hashes []string) map[string]string {
	result := make(map[string]string, len(hashes))
	for i, hash := range hashes {
		length := min(7, len(hash))
		for _, neighbor := range []int{i - 1, i + 1} {
			if neighbor < 0 || neighbor >= len(hashes) {
				continue
			}
			for length < len(hash) && strings.HasPrefix(hashes[neighbor], hash[:length]) {
				length++
			}
		}
		result[hash] = hash[:length]
	}
	return result
}

func splitMessage(message string) (string, string) {
	lines := strings.Split(strings.Trim(strings.ReplaceAll(message, "\r\n", "\n"), "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	var subject []string
	for len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		subject = append(subject, strings.TrimSpace(lines[0]))
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(subject, " "), strings.Join(lines, "\n")
}
