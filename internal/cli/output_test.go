package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteOutput(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "replace"}[exists], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notes.md")
			if exists {
				write(t, path, "old notes")
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeOutput(path, strings.NewReader("new notes")); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "new notes" {
				t.Fatalf("output %q: %v", data, err)
			}
			if exists && runtime.GOOS != "windows" {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("changed existing permissions: %v", info.Mode())
				}
			}
			assertNoTemporaryOutput(t, dir)
		})
	}
}

func TestWriteOutputFailurePreservesDestination(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new file", true: "existing file"}[exists], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notes.md")
			if exists {
				write(t, path, "keep existing notes")
			}
			failure := errors.New("simulated storage failure")
			reader := io.MultiReader(strings.NewReader("partial notes"), failingReader{failure})
			if err := writeOutput(path, reader); !errors.Is(err, failure) {
				t.Fatalf("expected write failure, got %v", err)
			}
			data, err := os.ReadFile(path)
			if exists {
				if err != nil || string(data) != "keep existing notes" {
					t.Fatalf("failed write changed destination: %q, %v", data, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed write created destination: %q, %v", data, err)
			}
			assertNoTemporaryOutput(t, dir)
		})
	}
}

func TestWriteOutputRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	reader := outputReaderFunc(func(p []byte) (int, error) {
		if err := os.Mkdir(path, 0o755); err != nil {
			return 0, err
		}
		return copy(p, "new notes"), io.EOF
	})
	if err := writeOutput(path, reader); err == nil {
		t.Fatal("expected replacement failure")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("failed replacement changed destination: %v", err)
	}
	assertNoTemporaryOutput(t, dir)
}

func TestWriteOutputSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	write(t, target, "old notes")
	link := filepath.Join(dir, "notes.md")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if err := writeOutput(link, strings.NewReader("new notes")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("output replaced symlink: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new notes" {
		t.Fatalf("symlink target %q: %v", data, err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := writeOutput(link, strings.NewReader("new notes")); err == nil {
		t.Fatal("expected an error for a dangling symlink")
	}
	info, err = os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("failed write replaced dangling symlink: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed write created symlink target: %v", err)
	}
	assertNoTemporaryOutput(t, dir)
}

func assertNoTemporaryOutput(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, ".arn-gen-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary output remains: %v, %v", files, err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type outputReaderFunc func([]byte) (int, error)

func (f outputReaderFunc) Read(p []byte) (int, error) { return f(p) }
