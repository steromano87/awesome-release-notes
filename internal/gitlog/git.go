package gitlog

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Commit is a commit as exposed to the templates; every exported field is part of the template API.
type Commit struct {
	// Hash is the full commit hash.
	Hash string
	// ShortHash is a prefix of at least seven characters, unique among the commits reachable from the end commit.
	ShortHash string
	// Subject is the first paragraph of the message, with its lines joined by spaces.
	Subject string
	// Body is the text after the subject, without the trailer block and the "---------" rule GitHub adds before it.
	Body string
	// Trailer is the unparsed trailer block (e.g. Signed-off-by, Co-authored-by lines), empty when absent.
	Trailer string
	// Message is the complete message: subject, body and trailer. Commit and exclusion patterns match against it.
	Message string
	// Author is the commit author.
	Author User
	// CoAuthors lists the Co-authored-by trailers; it is empty, never nil, when there are none.
	CoAuthors []User
	// Date is the author date, not the committer date.
	Date time.Time
}

// User is a person identified by name and email.
type User struct {
	Name  string
	Email string
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
		subject, full := splitMessage(commit.Message)
		message := subject
		if full != "" {
			message += "\n\n" + full
		}
		body, trailer := splitTrailer(full)
		hash := commit.Hash.String()
		result = append(result, Commit{
			Hash: hash, ShortHash: abbreviations[hash],
			Subject: subject, Body: body, Trailer: trailer, Message: message,
			Author: User{Name: commit.Author.Name, Email: commit.Author.Email}, Date: commit.Author.When,
			CoAuthors: coAuthors(trailer),
		})
	}
	return result, nil
}

// TagAt returns the name of the tag pointing at rev (HEAD when empty), or "" when there is none.
// With several tags, annotated ones win, then the most recent, then the lowest name.
func (r *Repository) TagAt(rev string) (string, error) {
	var end plumbing.Hash
	if rev == "" {
		head, err := r.repo.Head()
		if err != nil {
			return "", fmt.Errorf("repository must have at least one commit: %w", err)
		}
		end = head.Hash()
	} else {
		var err error
		if end, err = r.resolveCommit(rev); err != nil {
			return "", err
		}
	}
	refs, err := r.repo.Tags()
	if err != nil {
		return "", fmt.Errorf("read Git tags: %w", err)
	}
	defer refs.Close()
	var best string
	var bestAnnotated bool
	var bestDate time.Time
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		commit, annotated, date, err := r.peelTag(ref.Hash())
		if errors.Is(err, errNotCommit) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tag %q: %w", ref.Name().Short(), err)
		}
		if commit.Hash != end {
			return nil
		}
		name := ref.Name().Short()
		if best == "" || annotated && !bestAnnotated ||
			annotated == bestAnnotated && (date.After(bestDate) || date.Equal(bestDate) && name < best) {
			best, bestAnnotated, bestDate = name, annotated, date
		}
		return nil
	})
	return best, err
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

var (
	trailerLine  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*\s*:\s*\S`)
	coAuthorLine = regexp.MustCompile(`(?i)^co-authored-by\s*:\s*(.*?)\s*(?:<([^>]*)>)?\s*$`)
)

// splitTrailer separates a final paragraph made only of "Key: value" lines (with indented continuations) from the body.
func splitTrailer(body string) (string, string) {
	if body == "" {
		return "", ""
	}
	lines := strings.Split(body, "\n")
	start := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			start = i + 1
			break
		}
	}
	block := lines[start:]
	for i, line := range block {
		continuation := i > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"))
		if !continuation && !trailerLine.MatchString(line) {
			return body, ""
		}
	}
	return trimSeparator(strings.Join(lines[:start], "\n")), strings.Join(block, "\n")
}

var separatorLine = regexp.MustCompile(`^-{3,}$`)

// trimSeparator drops the horizontal rule (e.g. "---------") GitHub puts between a squash-merge body and its trailers.
func trimSeparator(body string) string {
	body = strings.Trim(body, "\n")
	lines := strings.Split(body, "\n")
	if last := len(lines) - 1; separatorLine.MatchString(strings.TrimSpace(lines[last])) && (last == 0 || strings.TrimSpace(lines[last-1]) == "") {
		return strings.Trim(strings.Join(lines[:last], "\n"), "\n")
	}
	return body
}

func coAuthors(trailer string) []User {
	result := []User{}
	for _, line := range strings.Split(trailer, "\n") {
		if m := coAuthorLine.FindStringSubmatch(line); m != nil && (m[1] != "" || m[2] != "") {
			result = append(result, User{Name: m[1], Email: m[2]})
		}
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
