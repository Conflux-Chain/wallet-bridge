package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestUnsafeDatabasePermissionsRejected(t *testing.T) {
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			s.Close()
			path := filepath.Join(dir, name)
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(context.Background(), dir); !errors.Is(err, platform.ErrInsecurePath) {
				if s != nil {
					s.Close()
				}
				t.Fatal("unsafe database permissions accepted", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatal("unsafe database permissions were silently changed", err)
			}
		})
	}
}
