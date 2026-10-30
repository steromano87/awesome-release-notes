package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"awesome-release-notes/internal/config"
	"awesome-release-notes/internal/testutil"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

func TestProgressLogs(t *testing.T) {
	for _, tt := range []struct {
		name       string
		tags       bool
		from       string
		detached   bool
		fileOutput bool
	}{
		{name: "fallback"},
		{name: "automatic tag", tags: true},
		{name: "explicit tag", tags: true, from: "v1", fileOutput: true},
		{name: "detached HEAD", detached: true},
		{name: "detached HEAD with tag", tags: true, detached: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.Repository(t)
			testutil.Commit(t, dir, "initial")
			if tt.tags {
				testutil.Tag(t, dir, "v1", false)
			}
			testutil.Commit(t, dir, "feat: earlier change")
			if tt.tags {
				testutil.Tag(t, dir, "v2", true)
				testutil.Checkout(t, dir, "unmerged", true)
				testutil.Commit(t, dir, "unmerged change")
				testutil.Tag(t, dir, "v99", false)
				testutil.Checkout(t, dir, "main", false)
			}
			hash := testutil.Commit(t, dir, "feat: latest change")
			if tt.detached {
				worktree, err := testutil.Open(t, dir).Worktree()
				if err != nil {
					t.Fatal(err)
				}
				if err := worktree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(hash)}); err != nil {
					t.Fatal(err)
				}
			}
			write(t, filepath.Join(dir, config.DefaultName+".yaml"), "{}")
			args := []string{"--repo", dir}
			if tt.from != "" {
				args = append(args, "--from", tt.from)
			}
			file := filepath.Join(dir, "notes.md")
			if tt.fileOutput {
				args = append(args, "--output", file)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr, "test"); code != 0 {
				t.Fatalf("exit %d: %s", code, &stderr)
			}
			lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
			if len(lines) != 5 {
				t.Fatalf("expected repository, branch, tag, generation and success logs: %s", &stderr)
			}
			repoPath, err := strconv.Unquote(strings.TrimPrefix(lines[0], "[arn-gen] [info] Repository: "))
			if err != nil {
				t.Fatalf("invalid repository log: %q: %v", lines[0], err)
			}
			loggedRoot, err := os.Stat(repoPath)
			if err != nil {
				t.Fatal(err)
			}
			expectedRoot, err := os.Stat(dir)
			if err != nil || !os.SameFile(loggedRoot, expectedRoot) {
				t.Fatalf("repository log does not identify the Git root: %q, %v", repoPath, err)
			}
			wantBranch := `[arn-gen] [info] Running on branch: "main"`
			if tt.detached {
				wantBranch = "[arn-gen] [info] Running on detached HEAD: " + hash
			}
			if lines[1] != wantBranch {
				t.Fatalf("unexpected branch log: %q", lines[1])
			}
			wantTag := "[arn-gen] [info] No reachable tag found; including all history from the first commit"
			if tt.tags {
				tag := "v2"
				if tt.from != "" {
					tag = tt.from
				}
				wantTag = `[arn-gen] [info] Previous tag: "` + tag + `" (excluded from the commit range)`
			}
			if lines[2] != wantTag || !strings.HasPrefix(lines[3], "[arn-gen] [info] Generating release notes from ") ||
				!strings.HasPrefix(lines[4], "[arn-gen] [ok] Release notes generated successfully") {
				t.Fatalf("unexpected logs: %s", &stderr)
			}
			if tt.fileOutput {
				data, err := os.ReadFile(file)
				if err != nil || !bytes.HasPrefix(data, []byte("# Release notes\n")) || bytes.Contains(data, []byte("[arn-gen]")) ||
					stdout.Len() != 0 || !strings.Contains(lines[4], "notes.md") {
					t.Fatalf("unexpected file output or logs: %q, %v; %s", data, err, &stderr)
				}
			} else if !strings.HasPrefix(stdout.String(), "# Release notes\n") || strings.Contains(stdout.String(), "[arn-gen]") {
				t.Fatalf("logs contaminated stdout: %s", &stdout)
			}
		})
	}
}

func TestGenerateEndToEnd(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1", false)
	testutil.Commit(t, dir, "feat: new option\n\nDescribe the new option.\n\n- Usage details")
	customConfig := filepath.Join(dir, config.DefaultName+".yaml")
	write(t, customConfig, "sectionHeaderLevel: 3\nitemHeaderLevel: 4\nitemTitleTemplate: 'Custom: {{ .Fields.description }}'\nitemBodyTemplate: '{{ .Body }}'\n")
	nested := filepath.Join(dir, "subdirectory")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", nested, "--title", "Release 2"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if !strings.HasPrefix(stdout.String(), "# Release 2\n") ||
		!strings.Contains(stdout.String(), "### Other changes\n\n#### Custom: feat: new option\n\nDescribe the new option.\n\n- Usage details\n") ||
		strings.Contains(stdout.String(), "initial") || !strings.Contains(stderr.String(), "[ok] Release notes generated successfully") {
		t.Fatalf("unexpected output: %s; stderr: %s", &stdout, &stderr)
	}
	file := filepath.Join(dir, "notes.md")
	want := stdout.String()
	stdout.Reset()
	if code := Run([]string{"--repo", dir, "--from", "v1", "--config", customConfig, "--output", file, "--format", "markdown", "--title", "Release 2"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != want || stdout.Len() != 0 {
		t.Fatalf("unexpected file output: %q, %v; stdout: %s", data, err, &stdout)
	}
}

func TestGenerateConfigurationFormats(t *testing.T) {
	for _, tt := range []struct {
		extension, text string
	}{
		{"json", `{"sectionHeaderLevel":3,"itemHeaderLevel":4,"categories":[{"name":"Changes","categories":[{"name":"Features","patterns":["^feat:"]}]}]}`},
		{"toml", "sectionHeaderLevel = 3\nitemHeaderLevel = 4\n[[categories]]\nname = 'Changes'\n[[categories.categories]]\nname = 'Features'\npatterns = ['^feat:']"},
	} {
		t.Run(tt.extension, func(t *testing.T) {
			dir := testutil.Repository(t)
			testutil.Commit(t, dir, "feat: new option\n\nDetails.")
			path := filepath.Join(dir, config.DefaultName+"."+tt.extension)
			write(t, path, tt.text)
			nested := filepath.Join(dir, "nested")
			if err := os.Mkdir(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, explicit := range []bool{false, true} {
				args := []string{"--repo", nested}
				if explicit {
					custom := filepath.Join(t.TempDir(), "custom."+tt.extension)
					write(t, custom, tt.text)
					args = append(args, "--config", custom)
				}
				var stdout, stderr bytes.Buffer
				code := Run(args, &stdout, &stderr, "test")
				want := "# Release notes\n\n### Changes\n\n#### Features\n\n##### feat: new option\n\nDetails.\n"
				if code != 0 || !strings.Contains(stderr.String(), "[ok]") || stdout.String() != want {
					t.Fatalf("explicit=%v: exit %d, stdout %q, stderr %q", explicit, code, &stdout, &stderr)
				}
			}
			write(t, path, "invalid configuration")
			output := filepath.Join(dir, "notes.md")
			write(t, output, "keep existing notes")
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"--repo", nested, "--output", output}, &stdout, &stderr, "test"); code != 1 || stdout.Len() != 0 {
				t.Fatalf("invalid configuration must fail without stdout: exit %d, %s", code, &stderr)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "keep existing notes" {
				t.Fatalf("invalid configuration changed output: %q, %v", data, err)
			}
		})
	}
}

func TestExampleConfiguration(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1", false)
	messages := []string{
		"feat(api)!: change response format",
		"feat: add command\n\nUse the new command from a terminal.",
		"feat(api): add endpoint\n\nAPI details.",
		"feat(cli): add option\n\nCLI details.",
		"fix(parser): handle multiline input",
		"docs: update guide",
		"ci: test platforms",
		"Merge branch 'feature'\n\nLonger description",
		"refactor: revise API\n\nBREAKING CHANGE: remove old API",
	}
	for _, message := range messages {
		testutil.Commit(t, dir, message)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", config.DefaultName+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), string(data))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", dir}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	text := stdout.String()
	for _, expected := range []string{
		"# Release notes", "Changes included in this release.",
		"## Breaking changes", "### feat(api)!: change response format",
		"### refactor: revise API\n\nBREAKING CHANGE: remove old API",
		"## Features", "### feat: add command\n\nUse the new command from a terminal.",
		"### API features\n\n#### feat(api): add endpoint\n\nAPI details.",
		"### CLI features\n\n#### feat(cli): add option\n\nCLI details.",
		"## Bug fixes", "### fix(parser): handle multiline input",
		"## Documentation", "### docs: update guide", "## Maintenance", "### ci: test platforms",
		"## Other changes", "### Merge branch 'feature'\n\nLonger description", "Thank you to all contributors!",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in output:\n%s", expected, text)
		}
	}
	if strings.Count(text, "revise API") != 1 || strings.Count(text, "Longer description") != 1 {
		t.Fatalf("unexpected duplication or missing body in output:\n%s", text)
	}
}

func TestExcludedPatternsEndToEnd(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1", false)
	testutil.Commit(t, dir, "Merge branch 'feature'")
	testutil.Commit(t, dir, "feat: keep\n\nVisible body.")
	testutil.Commit(t, dir, "feat: hidden\n\nskip-release-notes")
	cfg := filepath.Join(dir, config.DefaultName+".yaml")
	write(t, cfg, `
commitPattern: '^feat: (?P<description>[^\n]+)'
excludedPatterns: ['^Merge ', '(?m)^skip-release-notes$']
`)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", dir}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if stdout.String() != "# Release notes\n\n## Other changes\n\n### feat: keep\n\nVisible body.\n" {
		t.Fatalf("unexpected filtered release notes: %s", &stdout)
	}
	write(t, cfg, "commitPattern: '^feat: (?P<description>[^\\n]+)'")
	stdout.Reset()
	if code := Run([]string{"--repo", dir}, &stdout, &stderr, "test"); code != 1 ||
		!strings.Contains(stderr.String(), "does not match commitPattern") || stdout.Len() != 0 {
		t.Fatalf("without exclusions every commit must be parsed; exit %d: %s", code, &stderr)
	}
}

func TestNestedCategoriesEndToEnd(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Tag(t, dir, "v1", false)
	testutil.Commit(t, dir, "feat(cli): nested option\n\nNested details.")
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), `
categories:
  - name: Changes
    categories:
      - name: Features
        categories:
          - name: CLI
            patterns: ['^feat\(cli\):']
      - name: Empty
        patterns: ['^fix:']
`)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", dir}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	want := "# Release notes\n\n## Changes\n\n### Features\n\n#### CLI\n\n##### feat(cli): nested option\n\nNested details.\n"
	if stdout.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", &stdout, want)
	}
}

func TestIgnoreUnmatchedCommitsEndToEnd(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	testutil.Commit(t, dir, "feat: keep\n\nDetails.")
	testutil.Commit(t, dir, "chore: skip")
	testutil.Commit(t, dir, "chore: excluded")
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), `
commitPattern: '^feat: (?P<description>[^\n]+)'
ignoreUnmatchedCommits: true
excludedPatterns: ['^chore: excluded$']
itemTitleTemplate: '{{.Fields.description}}'
`)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", dir}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stderr.String(), "[ok]") {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if want := "# Release notes\n\n## Other changes\n\n### keep\n\nDetails.\n"; stdout.String() != want {
		t.Fatalf("got %q, want %q", &stdout, want)
	}
	if strings.Count(stderr.String(), "[skip]") != 2 || !strings.Contains(stderr.String(), `"initial"`) ||
		!strings.Contains(stderr.String(), `"chore: skip"`) || strings.Contains(stderr.String(), `"chore: excluded"`) {
		t.Fatalf("expected a log for every unmatched commit, but not exclusions: %s", &stderr)
	}
}

func TestRunArguments(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
		text string
	}{
		{"version", []string{"--version"}, 0, "1.2.3"},
		{"help", []string{"--help"}, 0, "Usage:"},
		{"format", []string{"--format", "html"}, 2, "unsupported format"},
		{"unknown flag", []string{"--unknown"}, 2, "unknown flag"},
		{"positional", []string{"extra"}, 2, "positional arguments"},
		{"missing value", []string{"--from"}, 2, "flag needs an argument"},
		{"missing title value", []string{"--title"}, 2, "flag needs an argument"},
		{"empty title", []string{"--title", ""}, 2, "--title must be a non-empty single line"},
		{"whitespace title", []string{"--title", " \t "}, 2, "--title must be a non-empty single line"},
		{"multiline title", []string{"--title", "Release\nnotes"}, 2, "--title must be a non-empty single line"},
		{"carriage return title", []string{"--title", "Release\rnotes"}, 2, "--title must be a non-empty single line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr, "1.2.3")
			if code != tt.code || !strings.Contains(stdout.String()+stderr.String(), tt.text) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, &stdout, &stderr)
			}
			if tt.code == 0 && stderr.Len() != 0 {
				t.Fatalf("help/version must not emit progress logs: %s", &stderr)
			}
		})
	}
}

func TestRunWithoutArguments(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), "{}")
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr, "test"); code != 0 || !strings.HasPrefix(stdout.String(), "# Release notes\n") {
		t.Fatalf("nil args must not inherit the process arguments; exit %d: %s", code, &stderr)
	}
}

func TestGenerationErrorsDoNotOverwriteOutput(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	cfg := filepath.Join(dir, config.DefaultName+".yaml")
	file := filepath.Join(dir, "notes.md")
	write(t, file, "keep existing notes")
	for _, tt := range []struct {
		name string
		yaml string
		args []string
		want string
	}{
		{"missing config", "", nil, "read configuration"},
		{"bad config", "commitPattern: '['", nil, "invalid commitPattern"},
		{"null template", "itemTitleTemplate: null", nil, "schema validation failed"},
		{"null nested patterns", "categories: [{name: Changes, patterns: null}]", nil, "schema validation failed"},
		{"bad body template", "itemBodyTemplate: '{{.Fields.missing}}'", nil, "render commit"},
		{"bad title template", "itemTitleTemplate: '{{.Fields.missing}}'", nil, "render commit"},
		{"empty rendered title", "itemTitleTemplate: '{{if false}}hidden{{end}}'", nil, "must render a non-empty single line"},
		{"unmatched commit", "commitPattern: '^feat:'", nil, "does not match"},
		{"bad section level", "sectionHeaderLevel: 7", nil, "maximum: got 7, want 6"},
		{"bad item level", "itemHeaderLevel: 0", nil, "minimum: got 0, want 1"},
		{"bad exclusion regex", "excludedPatterns: ['[']", nil, "invalid excludedPatterns[0]"},
		{"nested heading overflow", "itemHeaderLevel: 6\ncategories: [{name: Root, categories: [{name: Child, patterns: ['.*']}]}]", nil, "exceeds Markdown header level 6"},
		{"title removed from YAML", "title: Release", nil, `additional property "title"`},
		{"missing tag", "{}", []string{"--from", "missing"}, "resolve tag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.yaml != "" {
				write(t, cfg, tt.yaml)
			}
			var stdout, stderr bytes.Buffer
			args := append([]string{"--repo", dir, "--output", file}, tt.args...)
			if code := Run(args, &stdout, &stderr, "test"); code != 1 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("exit %d, stderr %s", code, &stderr)
			}
			if strings.Contains(stderr.String(), "[ok]") {
				t.Fatalf("generation failure must not log success: %s", &stderr)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "keep existing notes" || stdout.Len() != 0 {
				t.Fatalf("generation failure changed output: %q, %v", data, err)
			}
		})
	}
}

func TestWriteErrors(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), "{}")
	var stderr bytes.Buffer
	if code := Run([]string{"--repo", dir}, brokenWriter{}, &stderr, "test"); code != 1 ||
		!strings.Contains(stderr.String(), "write release notes") || strings.Contains(stderr.String(), "[ok]") {
		t.Fatalf("expected stdout write error, got exit %d: %s", code, &stderr)
	}
	stderr.Reset()
	if code := Run([]string{"--repo", dir, "--output", dir}, &bytes.Buffer{}, &stderr, "test"); code != 1 ||
		!strings.Contains(stderr.String(), "write release notes") || strings.Contains(stderr.String(), "[ok]") {
		t.Fatalf("expected file write error, got exit %d: %s", code, &stderr)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer unavailable")
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
