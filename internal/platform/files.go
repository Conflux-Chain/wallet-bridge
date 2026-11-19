package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrInsecurePath = errors.New("path is not private to the current user")

// DataDir resolves the default path without creating it.
func DataDir() (string, error) {
	base, err := dataHome()
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, "WalletBridge")
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("data directory must be absolute")
	}
	return path, nil
}

func EnsurePrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("data directory must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create data directory parent: %w", err)
	}
	if err := createDirectory(path); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create private directory: %w", err)
	}
	return CheckPrivateDir(path)
}

func CheckPrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("data directory must be absolute")
	}
	f, err := openPrivate(path, true)
	if err != nil {
		return err
	}
	return f.Close()
}

func OpenPrivateFile(path string) (*os.File, error) {
	return openPrivate(path, false)
}

// CreatePrivateFile creates a new file, setting permissions before any data is written.
// It never opens or replaces an existing file.
func CreatePrivateFile(path string) (*os.File, error) {
	f, err := createFile(path)
	if err != nil {
		return nil, err
	}
	if err := checkHandle(f, false); err != nil {
		f.Close()
		return nil, errors.Join(err, os.Remove(path))
	}
	return f, nil
}

func openPrivate(path string, directory bool) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("%s: %w (unexpected file type)", path, ErrInsecurePath)
	}
	f, err := openFile(path, directory)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err == nil && !os.SameFile(info, opened) {
		err = fmt.Errorf("%s: %w (file changed while opening)", path, ErrInsecurePath)
	}
	if err == nil {
		err = checkHandle(f, directory)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
