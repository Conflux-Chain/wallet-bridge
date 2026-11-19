//go:build windows || darwin

package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestCrashWriterHelper(t *testing.T) {
	dir := os.Getenv("WALLET_BRIDGE_STORE_CRASH_DIR")
	if dir == "" {
		return
	}
	s := openTestStore(t, dir)
	if _, err := s.db.Exec("PRAGMA cache_size=1"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	grant, _ := fixture(0)
	if _, err := tx.Exec("UPDATE browser_grants SET revoked_at=? WHERE id=?", grant.IssuedAt.Add(time.Hour).UnixMilli(), grant.ID); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 256; index++ {
		grant, digest := fixture(index)
		if _, err := tx.Exec("INSERT INTO browser_grants (id, token_hash, issued_at, expires_at) VALUES (?, ?, ?, ?)", grant.ID, digest[:], grant.IssuedAt.UnixMilli(), grant.ExpiresAt.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("uncommitted")
	io.Copy(io.Discard, os.Stdin)
}

func TestCrashRecoversUncommittedAuthorizationChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	grant, digest := fixture(0)
	if err := s.CreateBrowserGrant(context.Background(), grant, digest); err != nil {
		t.Fatal(err)
	}
	s.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCrashWriterHelper$")
	cmd.Env = append(os.Environ(), "WALLET_BRIDGE_STORE_CRASH_DIR="+dir)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "uncommitted\n" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("crash helper did not reach transaction: %v %s", err, diagnostics.String())
	}
	journal, err := os.ReadFile(filepath.Join(dir, DatabaseName+"-journal"))
	if err != nil || len(journal) < 8 || !bytes.Equal(journal[:8], []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}) {
		t.Fatal("test did not produce a hot rollback journal", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	s = openTestStore(t, dir)
	if _, err := s.FindActiveBrowserGrant(context.Background(), digest, grant.IssuedAt.Add(2*time.Hour)); err != nil {
		t.Fatal("crash retained an uncommitted revocation", err)
	}
	_, newDigest := fixture(1)
	if _, err := s.FindActiveBrowserGrant(context.Background(), newDigest, grant.IssuedAt.Add(2*time.Hour)); !errors.Is(err, app.ErrBrowserGrantNotFound) {
		t.Fatal("crash retained an uncommitted grant", err)
	}
	grants, err := s.ListBrowserGrants(context.Background())
	if err != nil || len(grants) != 1 {
		t.Fatal("recovery lost committed data", err)
	}
	f, err := platform.OpenPrivateFile(filepath.Join(dir, DatabaseName+"-journal"))
	if err != nil {
		t.Fatal("recovered journal lost file protection", err)
	}
	f.Close()
}
