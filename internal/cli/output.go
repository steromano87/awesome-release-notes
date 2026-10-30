package cli

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func writeOutput(path string, contents io.Reader) (err error) {
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if info != nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output %q is not a regular file", path)
		}
		mode = info.Mode().Perm()
	}
	// Set permissions at creation so new files respect the process umask.
	file, err := os.OpenFile(filepath.Join(filepath.Dir(path), ".arn-gen-"+rand.Text()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if removeErr := os.Remove(file.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary output: %w", removeErr))
		}
	}()
	if _, err := io.Copy(file, contents); err != nil {
		return err
	}
	if info != nil {
		if err := file.Chmod(mode); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
