//go:build windows || darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDataDir(t *testing.T) {
	path, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "WalletBridge" {
		t.Fatalf("unexpected data path: %q", path)
	}
}

func TestPrivateFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	f, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("original"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePrivateFile(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing file must not be replaced: %v", err)
	}
	opened, err := OpenPrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatal("existing file contents changed")
	}
	if err := EnsurePrivateDir("relative"); err == nil {
		t.Fatal("relative data directory accepted")
	}
	if _, err := OpenPrivateFile(dir); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("directory accepted as credential file: %v", err)
	}
	if err := EnsurePrivateDir(path); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("file accepted as data directory: %v", err)
	}
}

func TestSymlinksRejected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(dir), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := EnsurePrivateDir(link); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("directory symlink accepted: %v", err)
	}
	path := filepath.Join(dir, "original")
	f, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	link = filepath.Join(dir, "file-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPrivateFile(link); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("file symlink accepted: %v", err)
	}
	if f, err := OpenPrivateRecord(link); err == nil {
		f.Close()
		t.Fatal("replaceable record followed a file symlink")
	}
}
