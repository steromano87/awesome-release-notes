package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steromano87/awesome-release-notes/internal/testutil"
)

func TestGenerator(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "first")
	second := testutil.Commit(t, dir, "second")
	testutil.Commit(t, dir, "third")
	if err := os.WriteFile(filepath.Join(dir, ".release-notes.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Generator{RepoPath: dir, To: second, Title: "Release"}.Generate()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "# Release") || !strings.Contains(got, "first") || !strings.Contains(got, "second") || strings.Contains(got, "third") {
		t.Fatalf("unexpected notes: %s", got)
	}
}

func TestGeneratorDefaultTitle(t *testing.T) {
	dir := testutil.Repository(t)
	testutil.Commit(t, dir, "first")
	testutil.Tag(t, dir, "v1.0.0", true)
	testutil.Commit(t, dir, "second")
	testutil.Tag(t, dir, "v2.0.0", false)
	testutil.Commit(t, dir, "third")
	if err := os.WriteFile(filepath.Join(dir, ".release-notes.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ to, want string }{{"v2.0.0", "# v2.0.0\n"}, {"", "# unknown\n"}} {
		out, err := Generator{RepoPath: dir, To: tt.to}.Generate()
		if err != nil || !strings.HasPrefix(string(out), tt.want) {
			t.Fatalf("to %q: want prefix %q, got %q, %v", tt.to, tt.want, out, err)
		}
	}
}
