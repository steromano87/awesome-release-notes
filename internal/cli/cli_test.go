package cli

import (
	"bytes"
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
		name     string
		tags     bool
		from     string
		detached bool
	}{
		{name: "fallback"},
		{name: "automatic tag", tags: true},
		{name: "explicit tag", tags: true, from: "v1"},
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
			code, notes, stdout, stderr := runNotes(t, args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if len(lines) != 5 || stderr != "" {
				t.Fatalf("expected repository, branch, tag, generation and success logs on stdout only: %s; stderr: %s", stdout, stderr)
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
				t.Fatalf("unexpected logs: %s", stdout)
			}
			if !strings.HasPrefix(notes, "# Release notes\n") || strings.Contains(notes, "[arn-gen]") || !strings.Contains(lines[4], "notes.md") {
				t.Fatalf("unexpected file output or logs: %q; %s", notes, stdout)
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
	code, notes, stdout, stderr := runNotes(t, "--repo", nested, "--title", "Release 2")
	if code != 0 || !strings.HasPrefix(notes, "# Release 2\n") ||
		!strings.Contains(notes, "### Other changes\n\n#### Custom: feat: new option\n\nDescribe the new option.\n\n- Usage details\n") ||
		strings.Contains(notes, "initial") || !strings.Contains(stdout, "[ok] Release notes generated successfully") {
		t.Fatalf("unexpected output: exit %d, %s; stdout: %s; stderr: %s", code, notes, stdout, stderr)
	}
	code, again, _, stderr := runNotes(t, "--repo", dir, "--from", "v1", "--config", customConfig, "--format", "markdown", "--title", "Release 2")
	if code != 0 || again != notes {
		t.Fatalf("unexpected output with explicit options: exit %d, %q; stderr: %s", code, again, stderr)
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
				code, notes, stdout, stderr := runNotes(t, args...)
				want := "# Release notes\n\n### Changes\n\n#### Features\n\n##### feat: new option\n\nDetails.\n"
				if code != 0 || !strings.Contains(stdout, "[ok]") || notes != want {
					t.Fatalf("explicit=%v: exit %d, notes %q, stdout %q, stderr %q", explicit, code, notes, stdout, stderr)
				}
			}
			write(t, path, "invalid configuration")
			output := filepath.Join(dir, "notes.md")
			write(t, output, "keep existing notes")
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"--repo", nested, output}, &stdout, &stderr, "test"); code != 1 || strings.Contains(stdout.String(), "[ok]") {
				t.Fatalf("invalid configuration must fail without success log: exit %d, %s", code, &stderr)
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
	code, text, _, stderr := runNotes(t, "--repo", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
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
	code, notes, _, stderr := runNotes(t, "--repo", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if notes != "# Release notes\n\n## Other changes\n\n### feat: keep\n\nVisible body.\n" {
		t.Fatalf("unexpected filtered release notes: %s", notes)
	}
	write(t, cfg, "commitPattern: '^feat: (?P<description>[^\\n]+)'")
	code, notes, _, stderr = runNotes(t, "--repo", dir)
	if code != 1 || !strings.Contains(stderr, "does not match commitPattern") || notes != "" {
		t.Fatalf("without exclusions every commit must be parsed; exit %d: %s", code, stderr)
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
	code, notes, _, stderr := runNotes(t, "--repo", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := "# Release notes\n\n## Changes\n\n### Features\n\n#### CLI\n\n##### feat(cli): nested option\n\nNested details.\n"
	if notes != want {
		t.Fatalf("got:\n%s\nwant:\n%s", notes, want)
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
	code, notes, stdout, stderr := runNotes(t, "--repo", dir)
	if code != 0 || !strings.Contains(stdout, "[ok]") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := "# Release notes\n\n## Other changes\n\n### keep\n\nDetails.\n"; notes != want {
		t.Fatalf("got %q, want %q", notes, want)
	}
	if strings.Count(stdout, "[skip]") != 2 || !strings.Contains(stdout, `"initial"`) ||
		!strings.Contains(stdout, `"chore: skip"`) || strings.Contains(stdout, `"chore: excluded"`) {
		t.Fatalf("expected a log for every unmatched commit, but not exclusions: %s", stdout)
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
		{"format", []string{"--format", "html", "out.md"}, 2, "unsupported format"},
		{"missing output", nil, 2, "output file path is required"},
		{"extra output", []string{"a.md", "b.md"}, 2, "output file path is required"},
		{"unknown flag", []string{"--unknown"}, 2, "unknown flag"},
		{"missing value", []string{"--from"}, 2, "flag needs an argument"},
		{"missing title value", []string{"--title"}, 2, "flag needs an argument"},
		{"empty title", []string{"out.md", "--title", ""}, 2, "--title must be a non-empty single line"},
		{"whitespace title", []string{"out.md", "--title", " \t "}, 2, "--title must be a non-empty single line"},
		{"multiline title", []string{"out.md", "--title", "Release\nnotes"}, 2, "--title must be a non-empty single line"},
		{"carriage return title", []string{"out.md", "--title", "Release\rnotes"}, 2, "--title must be a non-empty single line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr, "1.2.3")
			if code != tt.code || !strings.Contains(stdout.String()+stderr.String(), tt.text) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, &stdout, &stderr)
			}
			if tt.code == 0 && strings.Contains(stdout.String(), "[arn-gen]") {
				t.Fatalf("help/version must not emit progress logs: %s", &stdout)
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
	if code := Run([]string{"notes.md"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if data, err := os.ReadFile("notes.md"); err != nil || !strings.HasPrefix(string(data), "# Release notes\n") {
		t.Fatalf("output path must be relative to the working directory: %q, %v", data, err)
	}
	if code := Run(nil, &stdout, &stderr, "test"); code != 2 {
		t.Fatalf("nil args must not inherit the process arguments; exit %d", code)
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
		{"bad config", "commitPattern: '['", nil, "'commitPattern' error parsing regexp"},
		{"empty template", "itemTitleTemplate: ''", nil, "'itemTitleTemplate' value must not be empty"},
		{"nested patterns missing", "categories: [{name: Changes, patterns: null}]", nil, "at least one pattern"},
		{"bad body template", "itemBodyTemplate: '{{.Fields.missing}}'", nil, "render commit"},
		{"bad title template", "itemTitleTemplate: '{{.Fields.missing}}'", nil, "render commit"},
		{"empty rendered title", "itemTitleTemplate: '{{if false}}hidden{{end}}'", nil, "must render a non-empty single line"},
		{"unmatched commit", "commitPattern: '^feat:'", nil, "does not match"},
		{"bad section level", "sectionHeaderLevel: 7", nil, "sectionHeaderLevel must be"},
		{"bad item level", "itemHeaderLevel: 0", nil, "itemHeaderLevel must be"},
		{"bad exclusion regex", "excludedPatterns: ['[']", nil, "'excludedPatterns[0]' error parsing regexp"},
		{"nested heading overflow", "itemHeaderLevel: 6\ncategories: [{name: Root, categories: [{name: Child, patterns: ['.*']}]}]", nil, "exceeds Markdown header level 6"},
		{"title removed from YAML", "title: Release", nil, "invalid keys: title"},
		{"missing tag", "{}", []string{"--from", "missing"}, "resolve revision"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.yaml != "" {
				write(t, cfg, tt.yaml)
			}
			var stdout, stderr bytes.Buffer
			args := append([]string{"--repo", dir}, tt.args...)
			if code := Run(append(args, file), &stdout, &stderr, "test"); code != 1 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("exit %d, stderr %s", code, &stderr)
			}
			if strings.Contains(stdout.String(), "[ok]") {
				t.Fatalf("generation failure must not log success: %s", &stdout)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "keep existing notes" {
				t.Fatalf("generation failure changed output: %q, %v", data, err)
			}
		})
	}
}

func TestWriteErrors(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	write(t, filepath.Join(dir, config.DefaultName+".yaml"), "{}")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--repo", dir, dir}, &stdout, &stderr, "test"); code != 1 ||
		!strings.Contains(stderr.String(), "write release notes") || strings.Contains(stdout.String(), "[ok]") {
		t.Fatalf("expected file write error, got exit %d: %s", code, &stderr)
	}
}

// runNotes runs the CLI with an output file in a temporary directory and returns the written notes.
func runNotes(t *testing.T, args ...string) (code int, notes, stdout, stderr string) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "notes.md")
	var out, errOut bytes.Buffer
	code = Run(append(args, output), &out, &errOut, "test")
	data, _ := os.ReadFile(output)
	return code, string(data), out.String(), errOut.String()
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
