package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steromano87/awesome-release-notes/internal/config"
	"github.com/steromano87/awesome-release-notes/internal/testutil"
)

func run(args []string, stdout, stderr *bytes.Buffer) int {
	return testutil.Run(rootCmd, args, stdout, stderr)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
func TestRootArguments(t *testing.T) {
	rootCmd.Version = "1.2.3"
	for _, tt := range []struct {
		name string
		args []string
		code int
		text string
	}{
		{"version", []string{"--version"}, 0, "1.2.3"},
		{"help", []string{"--help"}, 0, "Available Commands:"},
		{"generate help", []string{"generate", "--help"}, 0, "--from"},
		{"unknown command", []string{"bogus"}, 1, `unknown command "bogus"`},
		{"unknown flag", []string{"--unknown"}, 2, "unknown flag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.code || !strings.Contains(stdout.String()+stderr.String(), tt.text) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, &stdout, &stderr)
			}
		})
	}
}

func TestInit(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "initial")
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.DefaultName+".yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--repo", nested}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, config.DefaultYAML) {
		t.Fatalf("expected the default configuration in the repository root: %v", err)
	}
	if code := run([]string{"check", "--repo", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("the generated configuration must be valid: exit %d: %s", code, &stderr)
	}
	write(t, path, "keep")
	stderr.Reset()
	if code := run([]string{"init", "--repo", dir}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("expected an error for an existing configuration, got exit %d: %s", code, &stderr)
	}
	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Fatalf("init overwrote the existing configuration: %q", data)
	}
	if code := run([]string{"init", "--repo", t.TempDir()}, &stdout, &stderr); code != 1 {
		t.Fatalf("expected an error outside a repository, got exit %d", code)
	}
}

func TestCheck(t *testing.T) {
	dir := testutil.Repository(t)
	path := filepath.Join(dir, config.DefaultName+".yaml")
	write(t, path, "{}")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--repo", dir}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "[ok] Configuration is valid") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, &stdout, &stderr)
	}
	custom := filepath.Join(dir, "custom.yaml")
	write(t, custom, "sectionHeaderLevel: 7")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", "--repo", dir, "--config", custom}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "sectionHeaderLevel must be") || strings.Contains(stdout.String(), "[ok]") {
		t.Fatalf("expected a validation error, got exit %d, stdout %q, stderr %q", code, &stdout, &stderr)
	}
	if code := run([]string{"check", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("expected an argument error, got exit %d", code)
	}
}
