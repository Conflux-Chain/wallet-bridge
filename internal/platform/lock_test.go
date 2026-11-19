//go:build windows || darwin

package platform

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
)

func TestServiceLockProcessHelper(t *testing.T) {
	dir := os.Getenv("WALLET_BRIDGE_LOCK_TEST_DIR")
	if dir == "" {
		return
	}
	lock, err := AcquireServiceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceLockAcrossProcesses(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%t", crash), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestServiceLockProcessHelper$")
			cmd.Env = append(os.Environ(), "WALLET_BRIDGE_LOCK_TEST_DIR="+dir)
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
			if err != nil || line != "locked\n" {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("lock holder did not start: %v %s", err, diagnostics.String())
			}
			if lock, err := AcquireServiceLock(dir); !errors.Is(err, ErrLocked) {
				if lock != nil {
					lock.Close()
				}
				t.Fatalf("contender unexpectedly acquired lock: %v", err)
			}
			if locked, err := ServiceLocked(dir); err != nil || !locked {
				t.Fatalf("cannot observe another process's lock: %v", err)
			}
			if crash {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				input.Close()
			}
			if err := cmd.Wait(); err != nil && !crash {
				t.Fatalf("lock holder failed: %v %s", err, diagnostics.String())
			}
			lock, err := AcquireServiceLock(dir)
			if err != nil {
				t.Fatal("process exit did not release lock:", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			if locked, err := ServiceLocked(dir); err != nil || locked {
				t.Fatalf("released lock still appears held: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "service.lock")); err != nil {
				t.Fatal("lock file must remain after release", err)
			}
		})
	}
}
