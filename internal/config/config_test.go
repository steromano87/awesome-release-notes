package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSchemaRejectsNullValues(t *testing.T) {
	typ := reflect.TypeFor[Config]()
	for i := 0; i < typ.NumField(); i++ {
		key := typ.Field(i).Tag.Get("mapstructure")
		for _, format := range []string{"yaml", "yml", "json"} {
			t.Run(key+"/"+format, func(t *testing.T) {
				text := key + ": null"
				if format == "json" {
					text = `{"` + key + `":null}`
				}
				_, err := load([]byte(text), format)
				if err == nil || !strings.Contains(err.Error(), "schema validation failed") ||
					!strings.Contains(strings.ToLower(err.Error()), strings.ToLower(key)) {
					t.Fatalf("null %s must fail with its field path: %v", key, err)
				}
			})
		}
	}
	for _, text := range []string{
		"itemTitleTemplate:",
		"excludedPatterns: [null]",
		"categories: [null]",
		"categories: [{name: null, patterns: [feat]}]",
		"categories: [{name: Features, patterns: null}]",
		"categories: [{name: Features, patterns: [null]}]",
		"categories: [{name: Features, categories: null}]",
		"categories: [{name: Features, categories: [{name: Child, patterns: null}]}]",
	} {
		t.Run(text, func(t *testing.T) {
			_, err := Load(strings.NewReader(text))
			if err == nil || !strings.Contains(err.Error(), "schema validation failed") {
				t.Fatalf("nested or implicit null must fail: %v", err)
			}
		})
	}
}

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
			cfg, err := load(text, format)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Frontmatter != "Before" || cfg.SectionHeaderLevel != 3 ||
				len(cfg.Groups) != 1 || cfg.Groups[0].Name != "Changes" ||
				len(cfg.Groups[0].Categories) != 1 || cfg.Groups[0].Categories[0].Name != "Features" {
				t.Fatalf("mixed-case keys changed configuration: %+v", cfg)
			}
		})
	}
}

func TestSchemaValidationPrecedesProcessing(t *testing.T) {
	for format, text := range map[string]string{
		"yaml": "commitPattern: '['\nitemTitleTemplate: null",
		"json": `{"commitPattern":"[","itemTitleTemplate":null}`,
		"toml": "commitPattern = '['\nitemTitleTemplate = false",
	} {
		t.Run(format, func(t *testing.T) {
			_, err := load([]byte(text), format)
			if err == nil || !strings.Contains(err.Error(), "schema validation failed") ||
				strings.Contains(err.Error(), "invalid commitPattern") {
				t.Fatalf("schema validation must run before regex compilation: %v", err)
			}
		})
	}
}

func TestSchemaConstraintsAllFormats(t *testing.T) {
	tests := []struct {
		name, json, toml, path string
	}{
		{"unknown key", `{"unknown":true}`, "unknown = true", "unknown"},
		{"wrong type", `{"frontmatter":42}`, "frontmatter = 42", "frontmatter"},
		{"low section level", `{"sectionHeaderLevel":0}`, "sectionHeaderLevel = 0", "sectionheaderlevel"},
		{"high section level", `{"sectionHeaderLevel":7}`, "sectionHeaderLevel = 7", "sectionheaderlevel"},
		{"low item level", `{"itemHeaderLevel":0}`, "itemHeaderLevel = 0", "itemheaderlevel"},
		{"high item level", `{"itemHeaderLevel":7}`, "itemHeaderLevel = 7", "itemheaderlevel"},
		{"empty regex", `{"commitPattern":""}`, "commitPattern = ''", "commitpattern"},
		{"empty exclusion", `{"excludedPatterns":[""]}`, "excludedPatterns = ['']", "excludedpatterns[0]"},
		{"empty category pattern", `{"categories":[{"name":"Fixes","patterns":[""]}]}`,
			"[[categories]]\nname = 'Fixes'\npatterns = ['']", "categories[0].patterns[0]"},
		{"unknown nested key", `{"categories":[{"name":"Fixes","patterns":["fix"],"unknown":true}]}`,
			"[[categories]]\nname = 'Fixes'\npatterns = ['fix']\nunknown = true", "unknown"},
	}
	for _, tt := range tests {
		for _, format := range []string{"json", "yaml", "yml", "toml"} {
			t.Run(tt.name+"/"+format, func(t *testing.T) {
				text := tt.json
				if format == "toml" {
					text = tt.toml
				}
				_, err := load([]byte(text), format)
				if err == nil || !strings.Contains(err.Error(), "schema validation failed") ||
					!strings.Contains(strings.ToLower(err.Error()), tt.path) {
					t.Fatalf("expected schema error with field path %q, got %v", tt.path, err)
				}
			})
		}
	}
}

func TestSchemaHeaderBoundariesAllFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml", "yml", "toml"} {
		t.Run(format, func(t *testing.T) {
			text := `{"sectionHeaderLevel":1,"itemHeaderLevel":6}`
			if format == "toml" {
				text = "sectionHeaderLevel = 1\nitemHeaderLevel = 6"
			}
			cfg, err := load([]byte(text), format)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SectionHeaderLevel != 1 || cfg.ItemHeaderLevel != 6 {
				t.Fatalf("unexpected header levels: %+v", cfg.Config)
			}
		})
	}
}

func TestSchemaYAMLAliasesAndMerges(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
frontmatter: &text Introduction
backmatter: *text
categories:
  - &feature
    name: Features
    patterns: &patterns ['^feat:']
  - <<: *feature
    name: More features
    patterns: *patterns
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Frontmatter != "Introduction" || cfg.Backmatter != "Introduction" ||
		len(cfg.Groups) != 2 || cfg.Groups[1].Name != "More features" ||
		len(cfg.Groups[1].Patterns) != 1 || !cfg.Groups[1].Patterns[0].MatchString("feat: change") {
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
	want, err := Load(strings.NewReader(yaml))
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
				if !reflect.DeepEqual(got.Config, want.Config) || !reflect.DeepEqual(got.Groups, want.Groups) ||
					len(got.Exclusions) != 1 || !got.Exclusions[0].MatchString("Merge feature") {
					t.Fatalf("path %q: inconsistent configuration: %+v", path, got)
				}
				if !got.Groups[0].Categories[0].Patterns[0].MatchString("FEAT: new feature") {
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
	if err != nil || cfg.Frontmatter != "JSON" {
		t.Fatalf("expected Viper extension precedence: %+v, %v", cfg, err)
	}
	t.Chdir(root)
	writeConfig(t, "custom.toml", `frontmatter = "Explicit"`)
	cfg, err = LoadFile(t.TempDir(), "custom.toml")
	if err != nil || cfg.Frontmatter != "Explicit" {
		t.Fatalf("explicit paths must be relative to working directory: %+v, %v", cfg, err)
	}
	writeConfig(t, jsonPath, "{")
	if _, err := LoadFile(root, ""); err == nil {
		t.Fatal("invalid preferred file must not fall back to another format")
	}
}

func TestLoadFileUnmarshalDefaults(t *testing.T) {
	want, err := Load(strings.NewReader("{}"))
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
			if !reflect.DeepEqual(cfg.Config, want.Config) {
				t.Fatalf("unmarshalling changed defaults: %+v", cfg.Config)
			}
		})
	}
}

func TestLoadJSONIntegerHeaders(t *testing.T) {
	cfg, err := load([]byte(`{"sectionHeaderLevel":3.0,"itemHeaderLevel":4}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SectionHeaderLevel != 3 || cfg.ItemHeaderLevel != 4 {
		t.Fatalf("unexpected unmarshalled levels: %+v", cfg.Config)
	}
}

func TestLoadFileInvalid(t *testing.T) {
	tests := []struct {
		name, extension, text, want string
	}{
		{"unsupported", "xml", "<config/>", "unsupported configuration format"},
		{"null JSON", "json", "null", "got null, want object"},
		{"array JSON", "json", "[]", "got array, want object"},
		{"YAML in JSON file", "json", "frontmatter: Before", "decode JSON"},
		{"trailing JSON", "json", "{} {}", "decode JSON"},
		{"unknown JSON key", "json", `{"title":"Removed"}`, `additional property "title"`},
		{"fractional JSON header", "json", `{"sectionHeaderLevel":2.5}`, "got number, want integer"},
		{"string JSON header", "json", `{"sectionHeaderLevel":"2"}`, "got string, want integer"},
		{"boolean JSON header", "json", `{"itemHeaderLevel":true}`, "got boolean, want integer"},
		{"fractional TOML header", "toml", "sectionHeaderLevel = 2.5", "got number, want integer"},
		{"float TOML header", "toml", "sectionHeaderLevel = 2.0", "header levels must be integers"},
		{"multiple YAML documents", "yml", "{}\n---\n{}", "too many documents"},
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

func TestLoadFileUnsupportedFormats(t *testing.T) {
	for _, extension := range []string{"hcl", "tfvars", "ini", "env", "dotenv", "properties", "props", "prop"} {
		t.Run(extension, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, DefaultName+"."+extension)
			writeConfig(t, path, "frontmatter=Unsupported")
			if _, err := LoadFile(root, path); err == nil || !strings.Contains(err.Error(), "unsupported configuration format") {
				t.Fatalf("unsupported explicit file must fail: %v", err)
			}
			if _, err := LoadFile(root, ""); err == nil || !strings.Contains(err.Error(), "not found") {
				t.Fatalf("unsupported format must not be discovered: %v", err)
			}
			writeConfig(t, filepath.Join(root, DefaultName+".yaml"), "frontmatter: YAML")
			cfg, err := LoadFile(root, "")
			if err != nil || cfg.Frontmatter != "YAML" {
				t.Fatalf("unsupported file must not affect YAML discovery: %+v, %v", cfg, err)
			}
		})
	}
}

func writeConfig(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(strings.NewReader("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UncategorizedTitle != "Other changes" ||
		cfg.CategoryCaseInsensitive ||
		cfg.IgnoreUnmatchedCommits ||
		cfg.ItemTitleTemplate != "{{ .Subject }}" || cfg.ItemBodyTemplate != "{{ .Body }}" ||
		cfg.SectionHeaderLevel != 2 || cfg.ItemHeaderLevel != 3 ||
		len(cfg.ExcludedPatterns) != 0 || len(cfg.Exclusions) != 0 ||
		cfg.Pattern == nil || cfg.TitleTemplate == nil || cfg.BodyTemplate == nil {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	match := cfg.Pattern.FindStringSubmatch("Add feature\n\nMore details")
	if len(match) != 2 || match[1] != "Add feature" {
		t.Fatalf("default description should only contain the subject: %v", match)
	}
}

func TestLoadNestedCategories(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
sectionHeaderLevel: 4
itemHeaderLevel: 4
categories:
  - name: Changes
    categories:
      - name: Features
        categories:
          - name: API
            patterns: ['^feat\(api\):', '(?m)^API change$']
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 1 || len(cfg.Groups[0].Patterns) != 0 || len(cfg.Groups[0].Categories) != 1 {
		t.Fatalf("unexpected root category: %+v", cfg.Groups)
	}
	parent := cfg.Groups[0].Categories[0]
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
		{"empty file", "", "required YAML document is missing"},
		{"comments only", "# no configuration", "required YAML document is missing"},
		{"empty document", "---\n", "got null, want object"},
		{"syntax", "frontmatter: [", "schema validation failed"},
		{"unknown key", "titel: Release", `additional property "titel"`},
		{"unknown nested key", "categories:\n  - name: Fixes\n    regex: fix", `additional property "regex"`},
		{"non-mapping", "null", "got null, want object"},
		{"array root", "[]", "got array, want object"},
		{"duplicate key", "frontmatter: A\nfrontmatter: B", `duplicate key "frontmatter"`},
		{"scalar patterns", "categories:\n  - name: Fixes\n    patterns: fix", "got string, want array"},
		{"invalid value type", "frontmatter: [Release]", "got array, want string"},
		{"string ignore unmatched option", "ignoreUnmatchedCommits: 'true'", "got string, want boolean"},
		{"numeric ignore unmatched option", "ignoreUnmatchedCommits: 1", "got number, want boolean"},
		{"string category case option", "categoryCaseInsensitive: 'true'", "got string, want boolean"},
		{"numeric category case option", "categoryCaseInsensitive: 1", "got number, want boolean"},
		{"multiple documents", "{}\n---\n{}", "too many documents"},
		{"removed title key", "title: Release", `additional property "title"`},
		{"empty fallback", "uncategorizedTitle: ''", "uncategorizedTitle must"},
		{"empty regex", "commitPattern: ''", "minLength: got 0, want 1"},
		{"invalid regex", "commitPattern: '['", "invalid commitPattern"},
		{"invalid exclusion regex", "excludedPatterns: ['[']", "invalid excludedPatterns[0]"},
		{"invalid later exclusion regex", "excludedPatterns: ['^Merge ', '[']", "invalid excludedPatterns[1]"},
		{"empty exclusion regex", "excludedPatterns: ['']", "minLength: got 0, want 1"},
		{"scalar exclusions", "excludedPatterns: '^Merge '", "got string, want array"},
		{"invalid exclusion type", "excludedPatterns: [123]", "got number, want string"},
		{"duplicate capture", "commitPattern: '(?P<x>a)(?P<x>b)'", "duplicate capture"},
		{"invalid title template", "itemTitleTemplate: '{{'", "invalid itemTitleTemplate"},
		{"empty title template", "itemTitleTemplate: '  '", "itemTitleTemplate must"},
		{"invalid body template", "itemBodyTemplate: '{{'", "invalid itemBodyTemplate"},
		{"empty body template", "itemBodyTemplate: '  '", "itemBodyTemplate must"},
		{"unsplit template key", "itemTemplate: '{{ .Body }}'", `additional property "itemTemplate"`},
		{"old pattern key", "commit_pattern: '.*'", `additional property "commit_pattern"`},
		{"old template key", "commit_template: '{{ .Body }}'", `additional property "commit_template"`},
		{"old fallback key", "uncategorized_title: Maintenance", `additional property "uncategorized_title"`},
		{"renamed template key", "commitTemplate: '{{ .Body }}'", `additional property "commitTemplate"`},
		{"zero section level", "sectionHeaderLevel: 0", "minimum: got 0, want 1"},
		{"negative section level", "sectionHeaderLevel: -1", "minimum: got -1, want 1"},
		{"large section level", "sectionHeaderLevel: 7", "maximum: got 7, want 6"},
		{"fractional section level", "sectionHeaderLevel: 2.5", "got number, want integer"},
		{"float section level", "sectionHeaderLevel: 2.0", "header levels must be integers"},
		{"string section level", "sectionHeaderLevel: '2'", "got string, want integer"},
		{"boolean section level", "sectionHeaderLevel: true", "got boolean, want integer"},
		{"zero item level", "itemHeaderLevel: 0", "minimum: got 0, want 1"},
		{"negative item level", "itemHeaderLevel: -1", "minimum: got -1, want 1"},
		{"large item level", "itemHeaderLevel: 7", "maximum: got 7, want 6"},
		{"fractional item level", "itemHeaderLevel: 3.5", "got number, want integer"},
		{"string item level", "itemHeaderLevel: '3'", "got string, want integer"},
		{"missing category name", "categories:\n  - patterns: [fix]", "'not' failed (path: categories[0])"},
		{"missing patterns", "categories:\n  - name: Fixes", "at least one pattern"},
		{"invalid category regex", "categories:\n  - name: Fixes\n    patterns: ['[']", "invalid pattern"},
		{"empty category regex", "categories:\n  - name: Fixes\n    patterns: ['']", "minLength: got 0, want 1"},
		{"duplicate category", "categories:\n  - name: Fixes\n    patterns: [fix]\n  - name: Fixes\n    patterns: [bug]", "duplicate category"},
		{"fallback collision", "categories:\n  - name: Other changes\n    patterns: [fix]", "duplicate category"},
		{"empty nested leaf", "categories: [{name: Parent, categories: [{name: Child}]}]", "at least one pattern or subcategory"},
		{"empty child list", "categories: [{name: Parent, categories: []}]", "at least one pattern or subcategory"},
		{"unknown child key", "categories: [{name: Parent, categories: [{name: Child, regex: fix}]}]", `additional property "regex"`},
		{"invalid child name", "categories: [{name: Parent, categories: [{name: '', patterns: [fix]}]}]", "category names must"},
		{"invalid child regex", "categories: [{name: Parent, categories: [{name: Child, patterns: ['[']}]}]", "invalid pattern in category \"Child\""},
		{"invalid parent regex", "categories: [{name: Parent, patterns: ['['], categories: [{name: Child, patterns: [fix]}]}]", "invalid pattern in category \"Parent\""},
		{"duplicate descendant name", "categories: [{name: Parent, categories: [{name: Parent, patterns: [fix]}]}]", "duplicate category"},
		{"duplicate name across branches", "categories: [{name: First, categories: [{name: Child, patterns: [fix]}]}, {name: Second, categories: [{name: Child, patterns: [feat]}]}]", "duplicate category"},
		{"nested fallback collision", "categories: [{name: Parent, categories: [{name: Other changes, patterns: [fix]}]}]", "duplicate category"},
		{"nested section overflow", "sectionHeaderLevel: 6\nitemHeaderLevel: 1\ncategories: [{name: Parent, categories: [{name: Child, patterns: [fix]}]}]", "exceeds Markdown header level 6"},
		{"nested item overflow", "sectionHeaderLevel: 1\nitemHeaderLevel: 6\ncategories: [{name: Parent, categories: [{name: Child, patterns: [fix]}]}]", "exceeds Markdown header level 6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}
