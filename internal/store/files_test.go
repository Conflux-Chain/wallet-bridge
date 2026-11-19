//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestDatabaseSymlinksRejected(t *testing.T) {
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "data")
			if err := platform.EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte("retain target"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Skip("symlink creation unavailable:", err)
			}
			if s, err := Open(context.Background(), dir); !errors.Is(err, platform.ErrInsecurePath) {
				if s != nil {
					s.Close()
				}
				t.Fatal("symlink accepted", err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "retain target" {
				t.Fatal("symlink target changed", err)
			}
		})
	}
}
