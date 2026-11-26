//go:build windows || darwin

package update

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

const testUpgradeID = "ad841234000000000000000000000001"

func TestUpgradeBackup(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "data")
	if err := credentials.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "backups")
	final := filepath.Join(root, testUpgradeID)
	lock, err := platform.AcquireServiceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateBackup(ctx, dir, testUpgradeID); !errors.Is(err, platform.ErrLocked) {
		t.Fatal("snapshot while service holds lock", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackup(ctx, dir, testUpgradeID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot of missing database", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("failed snapshot left files", entries, err)
	}
	s, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a process killed after staging private data and before rename.
	partial := filepath.Join(root, ".partial-"+testUpgradeID)
	if err := platform.EnsurePrivateDir(partial); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupFile(filepath.Join(partial, credentials.FileName), "incomplete"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackup(ctx, dir, testUpgradeID); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != testUpgradeID {
		t.Fatal("unexpected backup directories", entries, err)
	}
	if err := platform.CheckPrivateDir(final); err != nil {
		t.Fatal(err)
	}
	key, err := credentials.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved, err := credentials.Read(final); err != nil || saved != key {
		t.Fatal("saved credential differs", err)
	}
	db, err := os.ReadFile(filepath.Join(dir, store.DatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256([]byte(key+"\n")), credentials.FileName, sha256.Sum256(db), store.DatabaseName)
	if entries, err := os.ReadDir(final); err != nil || len(entries) != 3 {
		t.Fatal("unexpected backup contents", entries, err)
	}
	f, err := platform.OpenPrivateFile(filepath.Join(final, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(sums) != want {
		t.Fatal("checksum mismatch", err)
	}
	// A retry after live data changes must preserve the first backup.
	if err := os.WriteFile(filepath.Join(dir, credentials.FileName), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackup(ctx, dir, testUpgradeID); !errors.Is(err, os.ErrExist) {
		t.Fatal("retry did not preserve the existing snapshot", err)
	}
	if err := CreateBackup(ctx, dir, "ad841234000000000000000000000002"); !errors.Is(err, credentials.ErrInvalid) {
		t.Fatal("invalid source credential accepted", err)
	}
	if saved, err := credentials.Read(final); err != nil || saved != key {
		t.Fatal("original snapshot was changed", err)
	}
}
