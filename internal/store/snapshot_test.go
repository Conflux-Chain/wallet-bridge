//go:build windows || darwin

package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func snapshotDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "backup & # 钱包")
	if err := platform.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestUpgradeBackupRecordedResult(t *testing.T) {
	ctx := t.Context()
	dir, dest := snapshotDir(t), snapshotDir(t)
	source := openTestStore(t, dir)
	ref, session, now := resultFixture(t, source)
	result, err := source.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := source.ListBrowserGrants(ctx)
	if err != nil || len(grants) == 0 {
		t.Fatal("prepare browser grants", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(ctx, dir, dest); err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, dest)
	assertSessionUnchanged(t, s, session)
	got, err := s.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || !reflect.DeepEqual(got, result) || got.TxHash != walletHash {
		t.Fatal("snapshot lost transaction result", err)
	}
	gotGrants, err := s.ListBrowserGrants(ctx)
	if err != nil || !reflect.DeepEqual(gotGrants, grants) {
		t.Fatal("snapshot lost browser grants", err)
	}
}

func TestUpgradeBackupPreservesOlderSchema(t *testing.T) {
	dir, dest := snapshotDir(t), snapshotDir(t)
	s := openConnectionSchema(t, dir, 2)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, DatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(t.Context(), dir, dest); err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{dir, dest} {
		after, err := os.ReadFile(filepath.Join(location, DatabaseName))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("original schema or contents changed", err)
		}
	}
}

func TestUpgradeBackupRejectsInvalidDatabase(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		want      error
	}{
		{"foreign database", "PRAGMA application_id=123", ErrInvalidDatabase},
		{"future version", fmt.Sprintf("PRAGMA user_version=%d", len(migrations)+1), ErrSchemaTooNew},
		{"broken constraint", "PRAGMA ignore_check_constraints=ON; INSERT INTO browser_grants VALUES ('bad', zeroblob(32), 0, 2592000000, NULL)", nil},
		{"broken reference", "PRAGMA foreign_keys=OFF; INSERT INTO transaction_results VALUES ('missing','rejected','',0)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := snapshotDir(t)
			s := openTestStore(t, dir)
			if _, err := s.db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := Snapshot(t.Context(), dir, snapshotDir(t)); err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("unexpected snapshot result: %v", err)
			}
		})
	}
}
