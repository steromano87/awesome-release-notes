package notes

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	"awesome-release-notes/internal/config"
	"awesome-release-notes/internal/gitlog"
)

type TemplateItem struct {
	gitlog.Commit
	Fields map[string]string
}

func Render(cfg *config.Compiled, releaseTitle string, commits []gitlog.Commit, logger *log.Logger) ([]byte, error) {
	groups := make(map[string][]string)
commitLoop:
	for _, commit := range commits {
		for _, pattern := range cfg.Exclusions {
			if pattern.MatchString(commit.Message) {
				continue commitLoop
			}
		}
		matches := cfg.Pattern.FindStringSubmatch(commit.Message)
		if matches == nil && cfg.IgnoreUnmatchedCommits {
			if logger != nil {
				logger.Printf("[skip] Commit %s does not match commitPattern: %q", commit.ShortHash, commit.Subject)
			}
			continue
		}
		if strings.TrimSpace(commit.Subject) == "" {
			return nil, fmt.Errorf("commit %s has an empty subject", commit.ShortHash)
		}
		if matches == nil {
			return nil, fmt.Errorf("commit %s does not match commitPattern: %q", commit.ShortHash, commit.Subject)
		}
		fields := make(map[string]string)
		for i, name := range cfg.Pattern.SubexpNames() {
			if name != "" {
				fields[name] = matches[i]
			}
		}
		item := TemplateItem{Commit: commit, Fields: fields}
		var title, body bytes.Buffer
		if err := cfg.TitleTemplate.Execute(&title, item); err != nil {
			return nil, fmt.Errorf("render commit %s itemTitleTemplate: %w", commit.ShortHash, err)
		}
		renderedTitle := strings.Trim(title.String(), "\r\n")
		if strings.TrimSpace(renderedTitle) == "" || strings.ContainsAny(renderedTitle, "\r\n") {
			return nil, fmt.Errorf("itemTitleTemplate for commit %s must render a non-empty single line", commit.ShortHash)
		}
		if err := cfg.BodyTemplate.Execute(&body, item); err != nil {
			return nil, fmt.Errorf("render commit %s itemBodyTemplate: %w", commit.ShortHash, err)
		}
		group, depth := matchCategory(cfg.Groups, commit.Message, 0)
		if group == "" {
			group = cfg.UncategorizedTitle
		}
		text := strings.Repeat("#", cfg.ItemHeaderLevel+depth) + " " + renderedTitle
		if content := strings.Trim(body.String(), "\r\n"); strings.TrimSpace(content) != "" {
			text += "\n\n" + content
		}
		groups[group] = append(groups[group], text)
	}
	blocks := []string{"# " + releaseTitle}
	if text := strings.Trim(cfg.Frontmatter, "\r\n"); text != "" {
		blocks = append(blocks, text)
	}
	for _, category := range cfg.Groups {
		blocks = append(blocks, renderCategory(category, groups, cfg.SectionHeaderLevel, 0)...)
	}
	if entries := groups[cfg.UncategorizedTitle]; len(entries) > 0 {
		blocks = append(blocks, strings.Repeat("#", cfg.SectionHeaderLevel)+" "+cfg.UncategorizedTitle, strings.Join(entries, "\n\n"))
	}
	if text := strings.Trim(cfg.Backmatter, "\r\n"); text != "" {
		blocks = append(blocks, text)
	}
	document := strings.ReplaceAll(strings.Join(blocks, "\n\n")+"\n", "\r\n", "\n")
	return []byte(strings.ReplaceAll(document, "\r", "\n")), nil
}

func matchCategory(categories []config.CompiledCategory, message string, depth int) (string, int) {
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

func renderCategory(category config.CompiledCategory, groups map[string][]string, sectionLevel, depth int) []string {
	var children []string
	for _, child := range category.Categories {
		children = append(children, renderCategory(child, groups, sectionLevel, depth+1)...)
	}
	entries := groups[category.Name]
	if len(entries) == 0 && len(children) == 0 {
		return nil
	}
	blocks := []string{strings.Repeat("#", sectionLevel+depth) + " " + category.Name}
	if len(entries) > 0 {
		blocks = append(blocks, strings.Join(entries, "\n\n"))
	}
	return append(blocks, children...)
}
