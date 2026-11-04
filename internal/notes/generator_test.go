package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"awesome-release-notes/internal/testutil"
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
