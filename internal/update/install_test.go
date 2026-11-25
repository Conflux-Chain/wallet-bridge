//go:build windows || darwin

package update

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestInstallConcurrentAndConflict(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("HOME", profile)
	t.Setenv("LocalAppData", profile)
	dir, err := platform.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(profile, dir)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("expected isolated user data directory, got %q: %v", dir, err)
	}
	want := filepath.Join(dir, "bin", "wallet-bridge")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() {
			if path, err := Install(""); err != nil || path != want {
				t.Errorf("install = %q, %v", path, err)
			}
		})
	}
	group.Wait()
	if t.Failed() {
		return
	}
	if path, err := Install(dir); err != nil || path != want {
		t.Fatal("repeat install", path, err)
	}
	if err := os.WriteFile(want, []byte("another program"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err == nil {
		t.Fatal("expected a conflict")
	}
	data, err := os.ReadFile(want)
	if err != nil || string(data) != "another program" {
		t.Fatal("conflicting file changed", err)
	}
	files, err := os.ReadDir(filepath.Dir(want))
	if err != nil || len(files) != 1 || files[0].Name() != filepath.Base(want) {
		t.Fatal("unexpected installation files", files, err)
	}
}
