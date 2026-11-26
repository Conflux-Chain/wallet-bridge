//go:build windows || darwin

package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func publishSelection(dir string, state releaseSelection) error {
	return (activeSelection{releaseSelection: state}).publish(dir)
}

func writeSelection(t *testing.T, dir string, selected Candidate) string {
	t.Helper()
	active := filepath.Join(dir, "active")
	if err := platform.EnsurePrivateDir(active); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(active, "selection.json")
	f, err := platform.CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(map[string]string{"version": selected.Version, "sha256": selected.SHA256}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServiceProgramSelection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	versions := filepath.Join(dir, "versions")
	target := filepath.Join(versions, stagedVersion)
	for _, parent := range []string{dir, versions, target} {
		if err := platform.EnsurePrivateDir(parent); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ServiceProgram(dir); err != nil || got.Path != exe {
		t.Fatal("direct run", got, err)
	}
	installed, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	base, err := ServiceProgram(dir)
	if err != nil || base.Path != installed || !validSHA256(base.SHA256) {
		t.Fatal("installed baseline", base, err)
	}
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	selected := Candidate{Version: stagedVersion, Path: filepath.Join(target, name), SHA256: base.SHA256}
	record := writeSelection(t, dir, selected)
	if _, err := ServiceProgram(dir); err == nil {
		t.Fatal("missing enabled executable fell back to the installed program")
	}
	if err := os.Link(installed, selected.Path); err != nil {
		t.Fatal(err)
	}
	if got, err := ServiceProgram(dir); err != nil || got != selected {
		t.Fatal("enabled release", got, err)
	}
	for _, state := range []string{"{", `{"version":"../../other","sha256":"` + base.SHA256 + `"}`, `{"version":"` + stagedVersion + `","sha256":"` + strings.Repeat("0", 64) + `"}`} {
		if err := os.WriteFile(record, []byte(state), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ServiceProgram(dir); err == nil {
			t.Fatal("invalid selection accepted", state)
		}
	}
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceProgram(dir); err == nil {
		t.Fatal("incomplete selection fell back to the installed program")
	}
}

func TestPublishSelection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	installed, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	base, err := ServiceProgram(dir)
	if err != nil {
		t.Fatal(err)
	}
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	first := releaseSelection{Version: "v1.0.0", SHA256: base.SHA256}
	next := releaseSelection{Version: "v1.1.0", SHA256: base.SHA256}
	versions := filepath.Join(dir, "versions")
	if err := platform.EnsurePrivateDir(versions); err != nil {
		t.Fatal(err)
	}
	for _, state := range []releaseSelection{first, next} {
		target := filepath.Join(versions, state.Version)
		if err := platform.EnsurePrivateDir(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(installed, filepath.Join(target, name)); err != nil {
			t.Fatal(err)
		}
	}
	// An interrupted first publication must leave the normal fallback usable.
	staging := filepath.Join(dir, "active.next")
	if err := platform.EnsurePrivateDir(staging); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupFile(filepath.Join(staging, selectionFile), "{"); err != nil {
		t.Fatal(err)
	}
	if got, err := ServiceProgram(dir); err != nil || got != base {
		t.Fatal("partial selection affected startup", got, err)
	}
	if err := publishSelection(dir, first); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "active", selectionFile)
	old, err := platform.OpenPrivateRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	bad := next
	bad.SHA256 = strings.Repeat("0", 64)
	if err := publishSelection(dir, bad); err == nil {
		t.Fatal("selected an executable with the wrong checksum")
	}
	if got, err := ServiceProgram(dir); err != nil || got.Version != first.Version {
		t.Fatal("failed publication changed selection", got, err)
	}
	for range 2 {
		if err := publishSelection(dir, next); err != nil {
			t.Fatal(err)
		}
		if got, err := ServiceProgram(dir); err != nil || got.Version != next.Version || got.SHA256 != next.SHA256 {
			t.Fatal("startup did not resolve the selected candidate", got, err)
		}
	}
	var prior releaseSelection
	if err := json.NewDecoder(old).Decode(&prior); err != nil || prior != first {
		t.Fatal("replacement invalidated an in-flight reader", prior, err)
	}
}
