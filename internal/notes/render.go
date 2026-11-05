package notes

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"text/template"

	"github.com/steromano87/awesome-release-notes/internal/config"
	"github.com/steromano87/awesome-release-notes/internal/gitlog"
)

type TemplateItem struct {
	gitlog.Commit
	Fields map[string]string
}

// MatterTemplateData is the data available to the frontmatter and backmatter templates.
type MatterTemplateData struct {
	Title string
	// Category is the category name, empty for the release-level frontmatter and backmatter.
	Category string
}

// Renderer turns commits into a Markdown document according to its configuration.
type Renderer struct {
	cfg *config.Config
}

func NewRenderer(cfg *config.Config) *Renderer {
	return &Renderer{cfg: cfg}
}

func (r *Renderer) Render(releaseTitle string, commits []gitlog.Commit) ([]byte, error) {
	cfg := r.cfg
	groups := make(map[string][]string)
	for _, commit := range commits {
		group, text, err := r.renderCommit(commit)
		if err != nil {
			return nil, err
		}
		if text != "" {
			groups[group] = append(groups[group], text)
		}
	}
	var blocks []string
	if releaseTitle != "" {
		blocks = append(blocks, "# "+releaseTitle)
	}
	front, err := r.renderMatter(cfg.Frontmatter, releaseTitle, "")
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, front...)
	for _, category := range cfg.Categories {
		categoryBlocks, err := r.renderCategory(category, groups, 0, releaseTitle)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, categoryBlocks...)
	}
	if entries := groups[cfg.UncategorizedTitle]; len(entries) > 0 {
		blocks = append(blocks, strings.Repeat("#", cfg.SectionHeaderLevel)+" "+cfg.UncategorizedTitle, strings.Join(entries, "\n\n"))
	}
	back, err := r.renderMatter(cfg.Backmatter, releaseTitle, "")
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, back...)
	document := strings.ReplaceAll(strings.Join(blocks, "\n\n")+"\n", "\r\n", "\n")
	return []byte(strings.ReplaceAll(document, "\r", "\n")), nil
}

// renderMatter returns the rendered frontmatter or backmatter block, or no block when the template is unset or renders blank.
func (r *Renderer) renderMatter(tmpl *template.Template, title, category string) ([]string, error) {
	if tmpl == nil {
		return nil, nil
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, MatterTemplateData{Title: title, Category: category}); err != nil {
		return nil, fmt.Errorf("render %s template: %w", tmpl.Name(), err)
	}
	if text := strings.Trim(out.String(), "\r\n"); text != "" {
		return []string{text}, nil
	}
	return nil, nil
}

// renderCommit returns the category and Markdown of one commit; the text is empty when the commit is skipped.
func (r *Renderer) renderCommit(commit gitlog.Commit) (group, text string, err error) {
	cfg := r.cfg
	for _, pattern := range cfg.ExcludedPatterns {
		if pattern.MatchString(commit.Message) {
			return "", "", nil
		}
	}
	matches := cfg.CommitPattern.FindStringSubmatch(commit.Message)
	if matches == nil && cfg.IgnoreUnmatchedCommits {
		log.Printf("[skip] Commit %s does not match commitPattern: %q", commit.ShortHash, commit.Subject)
		return "", "", nil
	}
	if strings.TrimSpace(commit.Subject) == "" {
		return "", "", fmt.Errorf("commit %s has an empty subject", commit.ShortHash)
	}
	if matches == nil {
		return "", "", fmt.Errorf("commit %s does not match commitPattern: %q", commit.ShortHash, commit.Subject)
	}
	fields := make(map[string]string)
	for i, name := range cfg.CommitPattern.SubexpNames() {
		if name != "" {
			fields[name] = matches[i]
		}
	}
	item := TemplateItem{Commit: commit, Fields: fields}
	var title, body bytes.Buffer
	if err := cfg.ItemTitleTemplate.Execute(&title, item); err != nil {
		return "", "", fmt.Errorf("render commit %s itemTitleTemplate: %w", commit.ShortHash, err)
	}
	renderedTitle := strings.Trim(title.String(), "\r\n")
	if strings.TrimSpace(renderedTitle) == "" || strings.ContainsAny(renderedTitle, "\r\n") {
		return "", "", fmt.Errorf("itemTitleTemplate for commit %s must render a non-empty single line", commit.ShortHash)
	}
	if err := cfg.ItemBodyTemplate.Execute(&body, item); err != nil {
		return "", "", fmt.Errorf("render commit %s itemBodyTemplate: %w", commit.ShortHash, err)
	}
	group, depth := matchCategory(cfg.Categories, commit.Message, 0)
	if group == "" {
		group = cfg.UncategorizedTitle
	}
	text = strings.Repeat("#", cfg.ItemHeaderLevel+depth) + " " + renderedTitle
	if content := strings.Trim(body.String(), "\r\n"); strings.TrimSpace(content) != "" {
		text += "\n\n" + content
	}
	return group, text, nil
}

func matchCategory(categories []config.Category, message string, depth int) (string, int) {
	for _, category := range categories {
		if name, childDepth := matchCategory(category.Categories, message, depth+1); name != "" {
			return name, childDepth
		}
		for _, pattern := range category.Patterns {
			if pattern.MatchString(message) {
				return category.Name, depth
			}
		}
	}
	return "", 0
}

// renderCategory renders the heading, frontmatter, items and backmatter of a category, then its subcategories.
func (r *Renderer) renderCategory(category config.Category, groups map[string][]string, depth int, title string) ([]string, error) {
	var children []string
	for _, child := range category.Categories {
		blocks, err := r.renderCategory(child, groups, depth+1, title)
		if err != nil {
			return nil, err
		}
		children = append(children, blocks...)
	}
	entries := groups[category.Name]
	if len(entries) == 0 && len(children) == 0 {
		return nil, nil
	}
	blocks := []string{strings.Repeat("#", r.cfg.SectionHeaderLevel+depth) + " " + category.Name}
	if len(entries) > 0 {
		front, err := r.renderMatter(category.Frontmatter, title, category.Name)
		if err != nil {
			return nil, err
		}
		back, err := r.renderMatter(category.Backmatter, title, category.Name)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, front...)
		blocks = append(blocks, strings.Join(entries, "\n\n"))
		blocks = append(blocks, back...)
	}
	return append(blocks, children...), nil
}
