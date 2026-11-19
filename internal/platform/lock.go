package platform

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrLocked = errors.New("another service holds the data directory lock")

type ServiceLock struct{ file *os.File }

func AcquireServiceLock(dir string) (*ServiceLock, error) {
	if err := EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "service.lock")
	f, err := CreatePrivateFile(path)
	if errors.Is(err, os.ErrExist) {
		f, err = OpenPrivateFile(path)
	}
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return &ServiceLock{file: f}, nil
}

// Close releases the kernel lock but retains its file to avoid inode races.
func (l *ServiceLock) Close() error {
	return errors.Join(unlockFile(l.file), l.file.Close())
}

// ServiceLocked checks an existing lock without creating a directory or file.
func ServiceLocked(dir string) (bool, error) {
	if err := CheckPrivateDir(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	f, err := OpenPrivateFile(filepath.Join(dir, "service.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		if errors.Is(err, ErrLocked) {
			return true, nil
		}
		return false, err
	}
	return false, unlockFile(f)
}
