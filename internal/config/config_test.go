package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"
)

func TestSchemaCaseInsensitiveKeys(t *testing.T) {
	for _, format := range []string{"yaml", "json", "toml"} {
		t.Run(format, func(t *testing.T) {
			object := map[string]any{
				"FRONTMATTER": "Before", "SectionHeaderLevel": 3,
				"CATEGORIES": []any{map[string]any{
					"NAME": "Changes", "Categories": []any{map[string]any{
						"NaMe": "Features", "PATTERNS": []string{"^feat:"},
					}},
				}},
			}
			text, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if format == "toml" {
				text = []byte("FRONTMATTER = 'Before'\nSectionHeaderLevel = 3\n[[CATEGORIES]]\nNAME = 'Changes'\n[[CATEGORIES.Categories]]\nNaMe = 'Features'\nPATTERNS = ['^feat:']")
			}
			cfg, err := loadAs(t, string(text), format)
			if err != nil {
				t.Fatal(err)
			}
			if matter(cfg.Frontmatter) != "Before" || cfg.SectionHeaderLevel != 3 ||
				len(cfg.Categories) != 1 || cfg.Categories[0].Name != "Changes" ||
				len(cfg.Categories[0].Categories) != 1 || cfg.Categories[0].Categories[0].Name != "Features" {
				t.Fatalf("mixed-case keys changed configuration: %+v", cfg)
			}
		})
	}
}

func TestSchemaHeaderBoundariesAllFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml", "yml", "toml"} {
		t.Run(format, func(t *testing.T) {
			text := `{"sectionHeaderLevel":1,"itemHeaderLevel":6}`
			if format == "toml" {
				text = "sectionHeaderLevel = 1\nitemHeaderLevel = 6"
			}
			cfg, err := loadAs(t, text, format)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SectionHeaderLevel != 1 || cfg.ItemHeaderLevel != 6 {
				t.Fatalf("unexpected header levels: %+v", cfg)
			}
		})
	}
}

func TestSchemaYAMLAliasesAndMerges(t *testing.T) {
	cfg, err := loadAs(t, `
frontmatter: &text Introduction
backmatter: *text
categories:
  - &feature
    name: Features
    patterns: &patterns ['^feat:']
  - <<: *feature
    name: More features
    patterns: *patterns
`, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if matter(cfg.Frontmatter) != "Introduction" || matter(cfg.Backmatter) != "Introduction" ||
		len(cfg.Categories) != 2 || cfg.Categories[1].Name != "More features" ||
		len(cfg.Categories[1].Patterns) != 1 || !cfg.Categories[1].Patterns[0].MatchString("feat: change") {
		t.Fatalf("schema validation changed aliases or merges: %+v", cfg)
	}
}

func TestLoadFileFormats(t *testing.T) {
	const yaml = `frontmatter: Before
backmatter: After
sectionHeaderLevel: 3
itemHeaderLevel: 4
itemTitleTemplate: '{{ .Subject }}'
itemBodyTemplate: '{{ .Body }}'
excludedPatterns: ['^Merge ']
categoryCaseInsensitive: true
ignoreUnmatchedCommits: true
categories:
  - name: Changes
    categories:
      - name: Features
        patterns: ['^feat:']
`
	formats := map[string]string{
		"yaml": yaml, "yml": yaml,
		"json": `{"frontmatter":"Before","backmatter":"After","sectionHeaderLevel":3,"itemHeaderLevel":4,
"itemTitleTemplate":"{{ .Subject }}","itemBodyTemplate":"{{ .Body }}","excludedPatterns":["^Merge "],"categoryCaseInsensitive":true,"ignoreUnmatchedCommits":true,
"categories":[{"name":"Changes","categories":[{"name":"Features","patterns":["^feat:"]}]}]}`,
		"toml": `frontmatter = "Before"
backmatter = "After"
sectionHeaderLevel = 3
itemHeaderLevel = 4
itemTitleTemplate = "{{ .Subject }}"
itemBodyTemplate = "{{ .Body }}"
excludedPatterns = ["^Merge "]
categoryCaseInsensitive = true
ignoreUnmatchedCommits = true
[[categories]]
name = "Changes"
[[categories.categories]]
name = "Features"
patterns = ["^feat:"]
`,
	}
	want, err := loadAs(t, yaml, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	for extension, text := range formats {
		t.Run(extension, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, DefaultName+"."+extension)
			writeConfig(t, file, text)
			for _, path := range []string{"", file} {
				got, err := LoadFile(root, path)
				if err != nil {
					t.Fatalf("path %q: %v", path, err)
				}
				if matter(got.Frontmatter) != matter(want.Frontmatter) || got.ItemHeaderLevel != want.ItemHeaderLevel || !reflect.DeepEqual(got.Categories, want.Categories) ||
					len(got.ExcludedPatterns) != 1 || !got.ExcludedPatterns[0].MatchString("Merge feature") {
					t.Fatalf("path %q: inconsistent configuration: %+v", path, got)
				}
				if !got.Categories[0].Categories[0].Patterns[0].MatchString("FEAT: new feature") {
					t.Fatal("categoryCaseInsensitive must apply to nested patterns in every format")
				}
			}
		})
	}
}

func TestLoadFileDiscovery(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, filepath.Join(root, DefaultName+".yaml"), "frontmatter: YAML")
	jsonPath := filepath.Join(root, DefaultName+".json")
	writeConfig(t, jsonPath, `{"frontmatter":"JSON"}`)
	cfg, err := LoadFile(root, "")
	if err != nil || matter(cfg.Frontmatter) != "JSON" {
		t.Fatalf("expected Viper extension precedence: %+v, %v", cfg, err)
	}
	t.Chdir(root)
	writeConfig(t, "custom.toml", `frontmatter = "Explicit"`)
	cfg, err = LoadFile(t.TempDir(), "custom.toml")
	if err != nil || matter(cfg.Frontmatter) != "Explicit" {
		t.Fatalf("explicit paths must be relative to working directory: %+v, %v", cfg, err)
	}
	writeConfig(t, jsonPath, "{")
	if _, err := LoadFile(root, ""); err == nil {
		t.Fatal("invalid preferred file must not fall back to another format")
	}
}

func TestLoadFileUnmarshalDefaults(t *testing.T) {
	want, err := loadAs(t, "{}", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	for extension, text := range map[string]string{
		"yaml": "{}", "yml": "{}", "json": "{}", "toml": "",
	} {
		t.Run(extension, func(t *testing.T) {
			root := t.TempDir()
			writeConfig(t, filepath.Join(root, DefaultName+"."+extension), text)
			cfg, err := LoadFile(root, "")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CommitPattern.String() != want.CommitPattern.String() || cfg.SectionHeaderLevel != want.SectionHeaderLevel {
				t.Fatalf("unmarshalling changed defaults: %+v", cfg)
			}
		})
	}
}

func TestLoadJSONIntegerHeaders(t *testing.T) {
	cfg, err := loadAs(t, `{"sectionHeaderLevel":3.0,"itemHeaderLevel":4}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SectionHeaderLevel != 3 || cfg.ItemHeaderLevel != 4 {
		t.Fatalf("unexpected unmarshalled levels: %+v", cfg)
	}
}

func TestLoadFileInvalid(t *testing.T) {
	tests := []struct {
		name, extension, text, want string
	}{
		{"unsupported", "xml", "<config/>", "Unsupported Config Type"},
		{"array JSON", "json", "[]", "cannot unmarshal"},
		{"YAML in JSON file", "json", "frontmatter: Before", "read configuration"},
		{"trailing JSON", "json", "{} {}", "read configuration"},
		{"unknown JSON key", "json", `{"title":"Removed"}`, "invalid keys: title"},
		{"fractional JSON header", "json", `{"sectionHeaderLevel":2.5}`, "is not an integer"},
		{"string JSON header", "json", `{"sectionHeaderLevel":"2"}`, "expected type 'int'"},
		{"boolean JSON header", "json", `{"itemHeaderLevel":true}`, "expected type 'int'"},
		{"fractional TOML header", "toml", "sectionHeaderLevel = 2.5", "is not an integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "custom."+tt.extension)
			writeConfig(t, path, tt.text)
			_, err := LoadFile(root, path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
	root := t.TempDir()
	for _, path := range []string{"", filepath.Join(root, "missing.yaml")} {
		if _, err := LoadFile(root, path); err == nil || !strings.Contains(err.Error(), "read configuration") {
			t.Fatalf("missing configuration must fail: %v", err)
		}
	}
}

func loadAs(t *testing.T, text, extension string) (*Config, error) {
	t.Helper()
	root := t.TempDir()
	writeConfig(t, filepath.Join(root, DefaultName+"."+extension), text)
	return LoadFile(root, "")
}

func writeConfig(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := loadAs(t, "{}\n", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UncategorizedTitle != "Other changes" ||
		cfg.CategoryCaseInsensitive ||
		cfg.IgnoreUnmatchedCommits ||

		cfg.SectionHeaderLevel != 2 || cfg.ItemHeaderLevel != 3 ||
		len(cfg.ExcludedPatterns) != 0 ||
		cfg.CommitPattern == nil || cfg.ItemTitleTemplate == nil || cfg.ItemBodyTemplate == nil {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	match := cfg.CommitPattern.FindStringSubmatch("Add feature\n\nMore details")
	if len(match) != 2 || match[1] != "Add feature" {
		t.Fatalf("default description should only contain the subject: %v", match)
	}
}

func TestLoadNestedCategories(t *testing.T) {
	cfg, err := loadAs(t, `
sectionHeaderLevel: 4
itemHeaderLevel: 4
categories:
  - name: Changes
    categories:
      - name: Features
        categories:
          - name: API
            patterns: ['^feat\(api\):', '(?m)^API change$']
`, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Categories) != 1 || len(cfg.Categories[0].Patterns) != 0 || len(cfg.Categories[0].Categories) != 1 {
		t.Fatalf("unexpected root category: %+v", cfg.Categories)
	}
	parent := cfg.Categories[0].Categories[0]
	if len(parent.Patterns) != 0 || len(parent.Categories) != 1 {
		t.Fatalf("unexpected intermediate category: %+v", parent)
	}
	leaf := parent.Categories[0]
	if leaf.Name != "API" || len(leaf.Patterns) != 2 || !leaf.Patterns[0].MatchString("feat(api): add endpoint") ||
		!leaf.Patterns[1].MatchString("feat: change\n\nAPI change") {
		t.Fatalf("unexpected leaf category: %+v", leaf)
	}
}

func TestLoadInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"syntax", "frontmatter: [", "read configuration"},
		{"unknown key", "titel: Release", "invalid keys: titel"},
		{"unknown nested key", "categories:\n  - name: Fixes\n    regex: fix", "invalid keys: regex"},
		{"array root", "[]", "cannot unmarshal"},
		{"duplicate key", "frontmatter: A\nfrontmatter: B", "already defined"},
		{"scalar patterns", "categories:\n  - name: Fixes\n    patterns: fix", "must be an array or slice"},
		{"invalid value type", "frontmatter: [Release]", "'frontmatter' expected a map or struct, got \"slice\""},
		{"string ignore unmatched option", "ignoreUnmatchedCommits: 'true'", "expected type 'bool'"},
		{"numeric ignore unmatched option", "ignoreUnmatchedCommits: 1", "expected type 'bool'"},
		{"string category case option", "categoryCaseInsensitive: 'true'", "expected type 'bool'"},
		{"numeric category case option", "categoryCaseInsensitive: 1", "expected type 'bool'"},
		{"removed title key", "title: Release", "invalid keys: title"},
		{"empty fallback", "uncategorizedTitle: ''", "uncategorizedTitle must"},
		{"empty regex", "commitPattern: ''", "must not be empty"},
		{"invalid regex", "commitPattern: '['", "'commitPattern' error parsing regexp"},
		{"invalid exclusion regex", "excludedPatterns: ['[']", "excludedPatterns[0]"},
		{"invalid later exclusion regex", "excludedPatterns: ['^Merge ', '[']", "excludedPatterns[1]"},
		{"empty exclusion regex", "excludedPatterns: ['']", "must not be empty"},
		{"scalar exclusions", "excludedPatterns: '^Merge '", "must be an array or slice"},
		{"invalid exclusion type", "excludedPatterns: [123]", "excludedPatterns[0]"},
		{"duplicate capture", "commitPattern: '(?P<x>a)(?P<x>b)'", "duplicate capture"},
		{"invalid title template", "itemTitleTemplate: '{{'", "itemTitleTemplate"},
		{"empty title template", "itemTitleTemplate: '  '", "itemTitleTemplate"},
		{"invalid body template", "itemBodyTemplate: '{{'", "itemBodyTemplate"},
		{"empty body template", "itemBodyTemplate: '  '", "itemBodyTemplate"},
		{"unsplit template key", "itemTemplate: '{{ .Body }}'", "invalid keys: itemtemplate"},
		{"old pattern key", "commit_pattern: '.*'", "invalid keys: commit_pattern"},
		{"old template key", "commit_template: '{{ .Body }}'", "invalid keys: commit_template"},
		{"old fallback key", "uncategorized_title: Maintenance", "invalid keys: uncategorized_title"},
		{"renamed template key", "commitTemplate: '{{ .Body }}'", "invalid keys: committemplate"},
		{"zero section level", "sectionHeaderLevel: 0", "must be an integer from 1 to 6"},
		{"negative section level", "sectionHeaderLevel: -1", "must be an integer from 1 to 6"},
		{"large section level", "sectionHeaderLevel: 7", "must be an integer from 1 to 6"},
		{"fractional section level", "sectionHeaderLevel: 2.5", "is not an integer"},
		{"string section level", "sectionHeaderLevel: '2'", "expected type 'int'"},
		{"boolean section level", "sectionHeaderLevel: true", "expected type 'int'"},
		{"zero item level", "itemHeaderLevel: 0", "must be an integer from 1 to 6"},
		{"negative item level", "itemHeaderLevel: -1", "must be an integer from 1 to 6"},
		{"large item level", "itemHeaderLevel: 7", "must be an integer from 1 to 6"},
		{"fractional item level", "itemHeaderLevel: 3.5", "is not an integer"},
		{"string item level", "itemHeaderLevel: '3'", "expected type 'int'"},
		{"missing category name", "categories:\n  - patterns: [fix]", "category names must"},
		{"missing patterns", "categories:\n  - name: Fixes", "at least one pattern"},
		{"invalid category regex", "categories:\n  - name: Fixes\n    patterns: ['[']", "categories[0].patterns[0]' error parsing regexp"},
		{"empty category regex", "categories:\n  - name: Fixes\n    patterns: ['']", "categories[0].patterns[0]' value must not be empty"},
		{"duplicate category", "categories:\n  - name: Fixes\n    patterns: [fix]\n  - name: Fixes\n    patterns: [bug]", "duplicate category"},
		{"fallback collision", "categories:\n  - name: Other changes\n    patterns: [fix]", "duplicate category"},
		{"empty nested leaf", "categories: [{name: Parent, categories: [{name: Child}]}]", "at least one pattern or subcategory"},
		{"empty child list", "categories: [{name: Parent, categories: []}]", "at least one pattern or subcategory"},
		{"unknown child key", "categories: [{name: Parent, categories: [{name: Child, regex: fix}]}]", "invalid keys: regex"},
		{"invalid child name", "categories: [{name: Parent, categories: [{name: '', patterns: [fix]}]}]", "category names must"},
		{"invalid child regex", "categories: [{name: Parent, categories: [{name: Child, patterns: ['[']}]}]", "categories[0].categories[0].patterns[0]"},
		{"invalid parent regex", "categories: [{name: Parent, patterns: ['['], categories: [{name: Child, patterns: [fix]}]}]", "categories[0].patterns[0]"},
		{"duplicate descendant name", "categories: [{name: Parent, categories: [{name: Parent, patterns: [fix]}]}]", "duplicate category"},
		{"duplicate name across branches", "categories: [{name: First, categories: [{name: Child, patterns: [fix]}]}, {name: Second, categories: [{name: Child, patterns: [feat]}]}]", "duplicate category"},
		{"nested fallback collision", "categories: [{name: Parent, categories: [{name: Other changes, patterns: [fix]}]}]", "duplicate category"},
		{"nested section overflow", "sectionHeaderLevel: 6\nitemHeaderLevel: 1\ncategories: [{name: Parent, categories: [{name: Child, patterns: [fix]}]}]", "exceeds Markdown header level 6"},
		{"nested item overflow", "sectionHeaderLevel: 1\nitemHeaderLevel: 6\ncategories: [{name: Parent, categories: [{name: Child, patterns: [fix]}]}]", "exceeds Markdown header level 6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadAs(t, tt.yaml, "yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func matter(tmpl *template.Template) string {
	var out strings.Builder
	_ = tmpl.Execute(&out, nil)
	return out.String()
}
