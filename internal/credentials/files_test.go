//go:build windows || darwin

package credentials

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func readKeys(t *testing.T, dir string) [2]string {
	t.Helper()
	var keys [2]string
	for i, kind := range []Kind{Client, Control} {
		key, err := Read(dir, kind)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			t.Fatal("credential is not a 32-byte hexadecimal value")
		}
		keys[i] = key
	}
	return keys
}

func TestCredentialsSurviveReinitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	initial := readKeys(t, dir)
	if initial[0] == initial[1] {
		t.Fatal("client and control credentials must be independent")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if readKeys(t, dir) != initial {
		t.Fatal("credentials changed during reinitialization")
	}
	if err := os.Remove(filepath.Join(dir, "control.key")); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if keys := readKeys(t, dir); keys[0] != initial[0] || keys[1] == initial[1] {
		t.Fatal("partial initialization did not preserve the existing client credential")
	}
}

func TestInvalidFilesAreNotReplaced(t *testing.T) {
	for _, content := range []string{"truncated", string(bytes.Repeat([]byte("z"), 64)) + "\n", string(bytes.Repeat([]byte("A"), 64)) + "\n", string(bytes.Repeat([]byte("0"), 66))} {
		t.Run(fmt.Sprintf("length-%d-%c", len(content), content[0]), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			if err := platform.EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "control.key")
			f, err := platform.CreatePrivateFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(content); err != nil {
				t.Fatal(err)
			}
			f.Close()
			if err := Ensure(dir); !errors.Is(err, ErrInvalid) {
				t.Fatalf("damaged credential accepted: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != content {
				t.Fatal("damaged credential was overwritten")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("validation failure created extra files")
			}
		})
	}
}

func TestReadIsIndependentAndDoesNotInitialize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := Read(dir, Client); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory read: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created a directory")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "control.key"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, Client); err != nil {
		t.Fatal("client read depends on control credential:", err)
	}
	if _, err := Read(dir, Control); !errors.Is(err, ErrInvalid) {
		t.Fatalf("damaged control credential accepted: %v", err)
	}
	if _, err := Read(dir, Kind(255)); err == nil {
		t.Fatal("unknown credential kind accepted")
	}
}

func TestFailedInitializationCleansTemporaryFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	// Enough entropy for the first file only: the second attempt fails after
	// creating its temporary file. A retry must reuse the completed first file.
	if err := ensure(dir, bytes.NewReader(make([]byte, 32))); !errors.Is(err, io.EOF) {
		t.Fatalf("random source failure not returned: %v", err)
	}
	client, err := Read(dir, Client)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "client.key" {
		t.Fatal("failed initialization left temporary files")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if readKeys(t, dir)[0] != client {
		t.Fatal("retry replaced completed client credential")
	}
}

func TestConcurrentProcesses(t *testing.T) {
	if dir := os.Getenv("WALLET_BRIDGE_TEST_CREDENTIAL_DIR"); dir != "" {
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		if err := Ensure(dir); err != nil {
			t.Fatal(err)
		}
		keys := readKeys(t, dir)
		fmt.Printf("%x", sha256.Sum256([]byte(keys[0]+keys[1])))
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		input  io.WriteCloser
		output bytes.Buffer
	}
	children := make([]child, 6)
	for i := range children {
		c := &children[i]
		c.cmd = exec.CommandContext(ctx, executable, "-test.run=^TestConcurrentProcesses$")
		c.cmd.Env = append(os.Environ(), "WALLET_BRIDGE_TEST_CREDENTIAL_DIR="+dir)
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		c.input, err = c.cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i := range children {
		children[i].input.Close()
	}
	for i := range children {
		c := &children[i]
		if err := c.cmd.Wait(); err != nil {
			t.Errorf("initializer %d failed: %v: %s", i, err, c.output.String())
		}
	}
	keys := readKeys(t, dir)
	want := fmt.Sprintf("%xPASS\n", sha256.Sum256([]byte(keys[0]+keys[1])))
	for i := range children {
		if children[i].output.String() != want {
			t.Errorf("initializer %d observed a different credential pair", i)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("concurrent initialization left extra files")
	}
}
