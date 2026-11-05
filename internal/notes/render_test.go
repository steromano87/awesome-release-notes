package notes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"awesome-release-notes/internal/config"
	"awesome-release-notes/internal/gitlog"
)

func TestRender(t *testing.T) {
	cfg := load(t, `
frontmatter: |
  Highlights of this release.
backmatter: |
  Thanks to all contributors!
commitPattern: '(?s)^(?P<type>\w+)(?:\((?P<scope>[^)]+)\))?: (?P<description>[^\n]+)(?:\n\n.*)?$'
itemBodyTemplate: '{{ .Body }}'
uncategorizedTitle: Maintenance
categories:
  - name: Features
    patterns: ['^feat:', '^feat\(']
  - name: Fixes
    patterns: ['^fix:', '^feat:']
  - name: Documentation
    patterns: ['^docs:']
`)
	commits := []gitlog.Commit{
		commit("aaa", "fix: prevent crash", "Return a useful error."),
		commit("bbb", "feat(cli): add options", "Detailed explanation\n\n- First option\n- Second option"),
		commit("ccc", "feat: first match only", ""),
		commit("ddd", "chore: update dependencies", "Update the YAML parser."),
	}
	out, err := NewRenderer(cfg).Render("Release 1.2", commits)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Release 1.2\n\nHighlights of this release.\n\n" +
		"## Features\n\n### feat(cli): add options\n\nDetailed explanation\n\n- First option\n- Second option\n\n" +
		"### feat: first match only\n\n" +
		"## Fixes\n\n### fix: prevent crash\n\nReturn a useful error.\n\n" +
		"## Maintenance\n\n### chore: update dependencies\n\nUpdate the YAML parser.\n\nThanks to all contributors!\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRenderDefaultsAndEmptyRange(t *testing.T) {
	cfg := load(t, "{}\n")
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commit("abc", "Simple subject", "Body")})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# Release\n\n## Other changes\n\n### Simple subject\n\nBody\n" {
		t.Fatalf("unexpected default output: %s", out)
	}
	out, err = NewRenderer(cfg).Render("Release", nil)
	if err != nil || string(out) != "# Release\n" {
		t.Fatalf("unexpected empty range: %q, %v", out, err)
	}
}

func TestRenderLineEndings(t *testing.T) {
	for _, tt := range []struct {
		name, front, body, back string
	}{
		{"LF", "\n", "\n", "\n"},
		{"CRLF", "\r\n", "\r\n", "\r\n"},
		{"CR", "\r", "\r", "\r"},
		{"mixed", "\r\n", "\r", "\n"},
	} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_empty_%t", tt.name, empty), func(t *testing.T) {
				cfg := load(t, fmt.Sprintf("frontmatter: %q\nbackmatter: %q\nitemBodyTemplate: %q\n",
					"Before"+tt.front+"intro", "After"+tt.back+"thanks", "Details"+tt.front+"{{ .Body }}"))
				var commits []gitlog.Commit
				if !empty {
					commits = []gitlog.Commit{commit("abc", "A change", "Paragraph"+tt.body+tt.body+"- item"+tt.body+"```text"+tt.body+"code"+tt.body+"```")}
				}
				out, err := NewRenderer(cfg).Render("Release", commits)
				if err != nil {
					t.Fatal(err)
				}
				want := "# Release\n\nBefore\nintro"
				if !empty {
					want += "\n\n## Other changes\n\n### A change\n\nDetails\nParagraph\n\n- item\n```text\ncode\n```"
				}
				want += "\n\nAfter\nthanks\n"
				if string(out) != want || strings.ContainsRune(string(out), '\r') {
					t.Fatalf("got %q, want %q", out, want)
				}
			})
		}
	}
}

func TestRenderCategoryCaseInsensitive(t *testing.T) {
	for _, tt := range []struct {
		name, option string
		insensitive  bool
	}{
		{"omitted", "", false},
		{"disabled", "categoryCaseInsensitive: false\n", false},
		{"enabled", "categoryCaseInsensitive: true\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.option+`
commitPattern: '^feat'
excludedPatterns: ['^feat: SKIP$']
categories:
  - name: Features
    patterns: ['^feat: general$']
    categories:
      - name: API
        patterns: ['(?m)^api details$']
  - name: Later
    patterns: ['^feat:']
`)
			if cfg.CommitPattern.MatchString("FEAT: general") || cfg.ExcludedPatterns[0].MatchString("feat: skip") {
				t.Fatal("category option must not affect parsing or exclusions")
			}
			out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{
				commit("aaa", "feat: GENERAL", ""),
				commit("bbb", "feat: endpoint", "API DETAILS"),
				commit("ccc", "feat: skip", ""),
				commit("ddd", "feat: SKIP", ""),
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "# Release\n\n## Later\n\n### feat: GENERAL\n\n### feat: endpoint\n\nAPI DETAILS\n\n### feat: skip\n"
			if tt.insensitive {
				want = "# Release\n\n## Features\n\n### feat: GENERAL\n\n### API\n\n#### feat: endpoint\n\nAPI DETAILS\n\n## Later\n\n### feat: skip\n"
			}
			if string(out) != want {
				t.Fatalf("got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestRenderHeaderLevels(t *testing.T) {
	for section := 1; section <= 6; section++ {
		for item := 1; item <= 6; item++ {
			t.Run(fmt.Sprintf("section_%d_item_%d", section, item), func(t *testing.T) {
				cfg := load(t, fmt.Sprintf(`
sectionHeaderLevel: %d
itemHeaderLevel: %d
categories:
  - name: Features
    patterns: ['^feat:']
`, section, item))
				out, err := NewRenderer(cfg).Render("Release notes", []gitlog.Commit{
					commit("aaa", "feat: original subject", "Feature details."),
					commit("bbb", "Uncategorized original subject", "Other details."),
				})
				if err != nil {
					t.Fatal(err)
				}
				want := "# Release notes\n\n" +
					strings.Repeat("#", section) + " Features\n\n" +
					strings.Repeat("#", item) + " feat: original subject\n\nFeature details.\n\n" +
					strings.Repeat("#", section) + " Other changes\n\n" +
					strings.Repeat("#", item) + " Uncategorized original subject\n\nOther details.\n"
				if string(out) != want {
					t.Fatalf("got:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

func TestRenderNestedCategories(t *testing.T) {
	cfg := load(t, `
categories:
  - name: Changes
    categories:
      - name: Features
        patterns: ['^feat']
        categories:
          - name: API
            patterns: ['^feat\(api\):']
          - name: Shadowed API
            patterns: ['^feat\(api\):']
          - name: CLI
            patterns: ['^feat\(cli\):']
      - name: Fixes
        patterns: ['^fix:']
      - name: Empty group
        categories:
          - name: Empty leaf
            patterns: ['^not-present:']
  - name: Later features
    patterns: ['^feat']
`)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{
		commit("aaa", "fix: prevent crash", "Fix details."),
		commit("bbb", "feat(api): first endpoint", "First API details."),
		commit("ccc", "feat: general feature", "General details."),
		commit("ddd", "feat(cli): add option", "CLI details."),
		commit("eee", "feat(api): second endpoint", "Second API details."),
		commit("fff", "docs: update guide", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Release\n\n## Changes\n\n### Features\n\n" +
		"#### feat: general feature\n\nGeneral details.\n\n" +
		"#### API\n\n##### feat(api): first endpoint\n\nFirst API details.\n\n" +
		"##### feat(api): second endpoint\n\nSecond API details.\n\n" +
		"#### CLI\n\n##### feat(cli): add option\n\nCLI details.\n\n" +
		"### Fixes\n\n#### fix: prevent crash\n\nFix details.\n\n" +
		"## Other changes\n\n### docs: update guide\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRenderNestedCategoryVisibility(t *testing.T) {
	for _, tt := range []struct {
		name     string
		yaml     string
		subjects []string
		want     string
	}{
		{"direct commits only", "", []string{"parent: direct"}, "## Parent\n\n### parent: direct\n"},
		{"descendant commits only", "", []string{"child: nested"}, "## Parent\n\n### Child\n\n#### child: nested\n"},
		{"both", "", []string{"child: nested", "parent: direct"}, "## Parent\n\n### parent: direct\n\n### Child\n\n#### child: nested\n"},
		{"neither", "", []string{"docs: unmatched"}, "## Other changes\n\n### docs: unmatched\n"},
		{"excluded descendant", "excludedPatterns: ['^child:']\n", []string{"child: nested", "parent: direct"}, "## Parent\n\n### parent: direct\n"},
		{"all excluded", "excludedPatterns: ['.*']\n", []string{"parent: direct", "child: nested"}, ""},
		{"no commits", "", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.yaml+`
categories:
  - name: Parent
    patterns: ['^parent:']
    categories:
      - name: Child
        patterns: ['^child:']
`)
			var commits []gitlog.Commit
			for i, subject := range tt.subjects {
				commits = append(commits, commit(fmt.Sprint(i), subject, ""))
			}
			out, err := NewRenderer(cfg).Render("Release", commits)
			if err != nil {
				t.Fatal(err)
			}
			want := "# Release\n"
			if tt.want != "" {
				want += "\n" + tt.want
			}
			if string(out) != want {
				t.Fatalf("got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestRenderNestedSiblingPriority(t *testing.T) {
	cfg := load(t, `
categories:
  - name: First
    patterns: ['^feat:']
    categories:
      - name: Unmatched
        patterns: ['^fix:']
  - name: Second
    categories:
      - name: Later child
        patterns: ['^feat:']
`)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commit("abc", "feat: first sibling wins", "")})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# Release\n\n## First\n\n### feat: first sibling wins\n" {
		t.Fatalf("an earlier parent's fallback must precede later siblings: %s", out)
	}
}

func TestRenderNestedHeaderLimit(t *testing.T) {
	cfg := load(t, `
sectionHeaderLevel: 4
itemHeaderLevel: 4
categories:
  - name: Root
    categories:
      - name: Middle
        categories:
          - name: Leaf
            patterns: ['.*']
`)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commit("abc", "feat: deepest level", "")})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Release\n\n#### Root\n\n##### Middle\n\n###### Leaf\n\n###### feat: deepest level\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestRenderWithoutMatchingExclusions(t *testing.T) {
	for _, text := range []string{"{}", "excludedPatterns: []", "excludedPatterns: ['^not-present$']"} {
		cfg := load(t, text)
		out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{
			commit("aaa", "feat: keep", "Feature details."),
			commit("bbb", "Merge branch 'feature'", ""),
			commit("ccc", "chore: keep", "Maintenance details."),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "# Release\n\n## Other changes\n\n### feat: keep\n\nFeature details.\n\n" +
			"### Merge branch 'feature'\n\n### chore: keep\n\nMaintenance details.\n"
		if string(out) != want {
			t.Fatalf("configuration %q: got:\n%s\nwant:\n%s", text, out, want)
		}
	}
}

func TestRenderExclusions(t *testing.T) {
	cfg := load(t, `
commitPattern: '^feat: (?P<description>[^\n]+)'
excludedPatterns:
  - '^Merge '
  - '^chore:'
  - '(?m)^skip-release-notes$'
categories:
  - name: Features
    patterns: ['^feat:']
  - name: Merges
    patterns: ['^Merge ']
  - name: Maintenance
    patterns: ['^chore:']
`)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{
		commit("aaa", "feat: first", "First details."),
		commit("bbb", "Merge branch 'feature'", ""),
		commit("ccc", "chore: release", "Maintenance details."),
		commit("ddd", "feat: hidden", "Details.\n\nskip-release-notes"),
		commit("eee", "", "skip-release-notes"),
		commit("fff", "feat: last", "Last details."),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Release\n\n## Features\n\n### feat: first\n\nFirst details.\n\n" +
		"### feat: last\n\nLast details.\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRenderAllExcluded(t *testing.T) {
	cfg := load(t, `
frontmatter: Introduction.
backmatter: Thank you.
excludedPatterns: ['.*']
commitPattern: '^never-match$'
itemTitleTemplate: '{{.Missing}}'
itemBodyTemplate: '{{.Missing}}'
categories:
  - name: Changes
    patterns: ['.*']
`)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{
		commit("aaa", "feat: hidden", "Details."),
		commit("bbb", "", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# Release\n\nIntroduction.\n\nThank you.\n" {
		t.Fatalf("expected only release title, frontmatter and backmatter: %s", out)
	}
}

func TestRenderMatterTemplates(t *testing.T) {
	cfg := load(t, "frontmatter: 'Intro to {{ .Title }}'\nbackmatter: '{{ if .Title }}Bye {{ .Title }}{{ end }}'\n")
	out, err := NewRenderer(cfg).Render("Release 1", nil)
	if err != nil || string(out) != "# Release 1\n\nIntro to Release 1\n\nBye Release 1\n" {
		t.Fatalf("unexpected output: %q, %v", out, err)
	}
	out, err = NewRenderer(cfg).Render("", nil)
	if err != nil || string(out) != "Intro to \n" {
		t.Fatalf("expected no title and blank backmatter omitted: %q, %v", out, err)
	}
	if _, err := NewRenderer(load(t, "frontmatter: '{{ .Missing }}'")).Render("R", nil); err == nil {
		t.Fatal("expected error for an unknown field")
	}
}

func TestRenderTemplateContext(t *testing.T) {
	cfg := load(t, `
commitPattern: '(?s)^(?P<description>.+)$'
itemBodyTemplate: '{{.Hash}}|{{.ShortHash}}|{{.Subject}}|{{.Body}}|{{.Message}}|{{.Author.Name}}|{{.Date.Format "2006-01-02"}}|{{.Fields.description}}'
`)
	item := commit("abc", "subject", "body")
	item.Hash = "abcdef"
	item.Author = gitlog.User{Name: "Developer"}
	item.Date = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	out, err := NewRenderer(cfg).Render("Release notes", []gitlog.Commit{item})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Release notes\n\n## Other changes\n\n### subject\n\n" +
		"abcdef|abc|subject|body|subject\n\nbody|Developer|2026-01-02|subject\n\nbody\n"
	if string(out) != want {
		t.Fatalf("unexpected template context in output:\n%s", out)
	}
}

func TestRenderItemContentAndDefaultTitle(t *testing.T) {
	for _, tt := range []struct {
		name     string
		template string
		body     string
		want     string
	}{
		{"no body", "{{ .Body }}", "", ""},
		{"whitespace body", "{{ .Body }}", " \n\n", ""},
		{"empty rendered content", "{{if false}}hidden{{end}}", "Details.", ""},
		{"custom body formatting", "**Details**\n\n{{ .Body }}", "Description.", "**Details**\n\nDescription.\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, fmt.Sprintf("itemBodyTemplate: %q\n", tt.template))
			out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commit("abc", "feat(api)!: Keep original title", tt.body)})
			if err != nil {
				t.Fatal(err)
			}
			want := "# Release\n\n## Other changes\n\n### feat(api)!: Keep original title\n"
			if tt.want != "" {
				want += "\n" + tt.want
			}
			if string(out) != want {
				t.Fatalf("got %q, want %q", out, want)
			}
		})
	}
}

func TestRenderIndependentItemTemplates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		yaml  string
		title string
		body  string
	}{
		{"both omitted", "", "feat(api)!: Add options", "Original body.\n\n- Keep Markdown."},
		{"title only", "itemTitleTemplate: '{{.Fields.description}} ({{.ShortHash}})'", "Add options (abc)", "Original body.\n\n- Keep Markdown."},
		{"body only", "itemBodyTemplate: '**Details**: {{.Body}}'", "feat(api)!: Add options", "**Details**: Original body.\n\n- Keep Markdown."},
		{"both customized", "itemTitleTemplate: '{{.Fields.scope}}: {{.Fields.description}}'\nitemBodyTemplate: 'By {{.Author.Name}}: {{.Body}}'", "api: Add options", "By Developer: Original body.\n\n- Keep Markdown."},
		{"block title template", "itemTitleTemplate: |\n  {{.Subject}}\n", "feat(api)!: Add options", "Original body.\n\n- Keep Markdown."},
		{"independent definitions", `itemTitleTemplate: '{{define "value"}}Custom title{{end}}{{template "value"}}'
itemBodyTemplate: '{{define "value"}}Custom body{{end}}{{template "value"}}'`, "Custom title", "Custom body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, "commitPattern: '^feat\\((?P<scope>[^)]+)\\)!: (?P<description>[^\\n]+)'\n"+tt.yaml)
			item := commit("abc", "feat(api)!: Add options", "Original body.\n\n- Keep Markdown.")
			item.Author = gitlog.User{Name: "Developer"}
			out, err := NewRenderer(cfg).Render("Release notes", []gitlog.Commit{item})
			if err != nil {
				t.Fatal(err)
			}
			want := "# Release notes\n\n## Other changes\n\n### " + tt.title + "\n\n" + tt.body + "\n"
			if string(out) != want {
				t.Fatalf("got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestRenderErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		yaml string
		want string
	}{
		{"unmatched", "commitPattern: '^feat: (?P<description>.+)$'", "does not match commitPattern"},
		{"missing body capture", "itemBodyTemplate: '{{.Fields.missing}}'", "render commit abc itemBodyTemplate"},
		{"missing body field", "itemBodyTemplate: '{{.Missing}}'", "render commit abc itemBodyTemplate"},
		{"missing title capture", "itemTitleTemplate: '{{.Fields.missing}}'", "render commit abc itemTitleTemplate"},
		{"missing title field", "itemTitleTemplate: '{{.Missing}}'", "render commit abc itemTitleTemplate"},
		{"empty rendered title", "itemTitleTemplate: '{{if false}}hidden{{end}}'", "must render a non-empty single line"},
		{"whitespace rendered title", "itemTitleTemplate: '{{print \"  \"}}'", "must render a non-empty single line"},
		{"multiline rendered title", "itemTitleTemplate: \"first\\nsecond\"", "must render a non-empty single line"},
		{"carriage return in title", "itemTitleTemplate: \"first\\rsecond\"", "must render a non-empty single line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRenderer(load(t, tt.yaml)).Render("Release", []gitlog.Commit{commit("abc", "chore: test", "")})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
	_, err := NewRenderer(load(t, "{}")).Render("Release", []gitlog.Commit{commit("abc", "", "Body")})
	if err == nil || !strings.Contains(err.Error(), "empty subject") {
		t.Fatalf("expected error for an empty item title, got %v", err)
	}
}

func TestRenderIgnoreUnmatchedCommits(t *testing.T) {
	const base = `
commitPattern: '^feat: (?P<description>[^\n]+)'
itemTitleTemplate: '{{.Fields.description}}'
excludedPatterns: ['(?m)^skip-release-notes$']
categories:
  - name: Changes
    categories:
      - name: Features
        patterns: ['^feat:']
  - name: Unmatched
    patterns: ['^chore:']
`
	commits := []gitlog.Commit{
		commit("aaa", "chore: skip", ""),
		commit("bbb", "feat: keep", "Details."),
		commit("ccc", "feat: excluded", "skip-release-notes"),
		commit("ddd", "", ""),
	}
	for _, option := range []string{"", "ignoreUnmatchedCommits: false\n", "ignoreUnmatchedCommits: true\n"} {
		t.Run(strings.TrimSpace(option), func(t *testing.T) {
			cfg := load(t, option+base)
			out, err := NewRenderer(cfg).Render("Release", commits)
			if !cfg.IgnoreUnmatchedCommits {
				if err == nil || !strings.Contains(err.Error(), "does not match commitPattern") || out != nil {
					t.Fatalf("expected unchanged error behavior, got %q, %v", out, err)
				}
				return
			}
			want := "# Release\n\n## Changes\n\n### Features\n\n#### keep\n\nDetails.\n"
			if err != nil || string(out) != want {
				t.Fatalf("got %q, %v; want %q", out, err, want)
			}
		})
	}
	cfg := load(t, "ignoreUnmatchedCommits: true\nfrontmatter: Before\nbackmatter: After\n"+base)
	out, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commits[0], commits[2], commits[3]})
	if err != nil || string(out) != "# Release\n\nBefore\n\nAfter\n" {
		t.Fatalf("unexpected output when all commits are filtered: %q, %v", out, err)
	}
	cfg = load(t, "ignoreUnmatchedCommits: true\nitemBodyTemplate: '{{.Fields.missing}}'\ncommitPattern: '^feat:'")
	if _, err := NewRenderer(cfg).Render("Release", []gitlog.Commit{commits[1]}); err == nil || !strings.Contains(err.Error(), "itemBodyTemplate") {
		t.Fatalf("matching commits must still report template errors: %v", err)
	}
}

func commit(hash, subject, body string) gitlog.Commit {
	message := subject
	if body != "" {
		message += "\n\n" + body
	}
	return gitlog.Commit{ShortHash: hash, Subject: subject, Body: body, Message: message}
}

func load(t *testing.T, text string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile("", path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
