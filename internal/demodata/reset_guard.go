package demodata

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CheckDemoResetPath adds deletion guards for SQLite sidecars and directory
// symlinks. It is read-only and runs before canonicalization or deletion.
func CheckDemoResetPath(path string) error {
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == ".." {
			return ErrGuard
		}
	}
	if err := CheckDemoPath(path); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return ErrGuard
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !info.IsDir() {
			return ErrGuard
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return nil
}
