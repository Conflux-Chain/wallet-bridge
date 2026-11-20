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

func readKey(t *testing.T, dir string) string {
	t.Helper()
	key, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		t.Fatal("credential is not a 32-byte hexadecimal value")
	}
	return key
}

func TestCredentialsSurviveReinitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	initial := readKey(t, dir)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "client.key" {
		t.Fatal("new installation must create only the service credential")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if readKey(t, dir) != initial {
		t.Fatal("credential changed during reinitialization")
	}
}

func TestInvalidFilesAreNotReplaced(t *testing.T) {
	for _, content := range []string{"truncated", string(bytes.Repeat([]byte("z"), 64)) + "\n", string(bytes.Repeat([]byte("A"), 64)) + "\n", string(bytes.Repeat([]byte("0"), 66))} {
		t.Run(fmt.Sprintf("length-%d-%c", len(content), content[0]), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			if err := platform.EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "client.key")
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

func TestReadDoesNotInitialize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory read: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created a directory")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyControlFileIsIgnoredAndRetained(t *testing.T) {
	for _, legacy := range []string{string(bytes.Repeat([]byte("a"), 64)) + "\n", "broken"} {
		t.Run(fmt.Sprintf("length-%d", len(legacy)), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			if err := Ensure(dir); err != nil {
				t.Fatal(err)
			}
			initial := readKey(t, dir)
			path := filepath.Join(dir, "control.key")
			f, err := platform.CreatePrivateFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(legacy); err != nil {
				t.Fatal(err)
			}
			f.Close()
			if err := Ensure(dir); err != nil {
				t.Fatal("legacy file prevented initialization:", err)
			}
			if readKey(t, dir) != initial {
				t.Fatal("upgrade rotated the service credential")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != legacy {
				t.Fatal("upgrade changed the legacy file")
			}
		})
	}
}

func TestFailedInitializationCleansTemporaryFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := ensure(dir, bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("random source failure not returned: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed initialization left temporary files")
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	readKey(t, dir)
}

func TestConcurrentProcesses(t *testing.T) {
	if dir := os.Getenv("WALLET_BRIDGE_TEST_CREDENTIAL_DIR"); dir != "" {
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		if err := Ensure(dir); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%x", sha256.Sum256([]byte(readKey(t, dir))))
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
	want := fmt.Sprintf("%xPASS\n", sha256.Sum256([]byte(readKey(t, dir))))
	for i := range children {
		if children[i].output.String() != want {
			t.Errorf("initializer %d observed a different service credential", i)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("concurrent initialization left extra files")
	}
}
