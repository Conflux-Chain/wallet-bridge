package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// The upgrade writer owns a single rollback slot. A new upgrade can replace
// it only after the previous upgrade's record has been retired.
func (w *Upgrade) preparePrograms(ctx context.Context, id string, target releaseSelection) (Candidate, error) {
	if !upgradeIDPattern.MatchString(id) {
		return Candidate{}, errors.New("invalid upgrade ID")
	}
	if w.current != nil && (w.current.ID != id || w.current.Target != target) {
		return Candidate{}, UpgradeRecoveryRequired
	}
	if _, err := target.candidate(w.dir); err != nil {
		return Candidate{}, fmt.Errorf("verify target program: %w", err)
	}
	if w.current != nil {
		return (activeSelection{releaseSelection: w.current.Previous, Retained: true}).program(w.dir)
	}
	name, err := nativeAssetName()
	if err != nil {
		return Candidate{}, err
	}
	slot := filepath.Join(w.dir, "rollback-program")
	path := filepath.Join(slot, name)
	source, err := ServiceProgram(w.dir)
	if err != nil {
		return Candidate{}, err
	}
	saved, err := retainProgram(ctx, source, path)
	if err != nil {
		return Candidate{}, fmt.Errorf("save rollback program: %w", err)
	}
	if err := w.begin(id, releaseSelection{Version: saved.Version, SHA256: saved.SHA256}, target); err != nil {
		return Candidate{}, err
	}
	return saved, nil
}

func retainProgram(ctx context.Context, source Candidate, path string) (Candidate, error) {
	if err := platform.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return Candidate{}, err
	}
	input, err := os.Open(source.Path)
	if err != nil {
		return Candidate{}, err
	}
	defer input.Close()
	// Reclaim an incomplete copy left before the upgrade record was committed.
	next := path + ".next"
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Candidate{}, err
	}
	temp, err := platform.CreatePrivateFile(next)
	if err != nil {
		return Candidate{}, err
	}
	defer os.Remove(next)
	hash := sha256.New()
	if err := writeExecutable(temp, func(out io.Writer) error {
		_, err := io.Copy(io.MultiWriter(out, hash), input)
		return err
	}); err != nil {
		return Candidate{}, err
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	if source.SHA256 != "" && source.SHA256 != digest {
		return Candidate{}, errors.New("enabled program changed during copy")
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	// Windows may still be running this copy after a previous recovery.
	// Reuse identical bytes without trying to replace the running executable.
	if err := verifyExecutable(path, digest); err != nil {
		if err := platform.CommitRecord(next, path); err != nil {
			return Candidate{}, err
		}
		if err := verifyExecutable(path, digest); err != nil {
			return Candidate{}, err
		}
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(check, path, "version", "--json").Output()
	if err != nil {
		return Candidate{}, fmt.Errorf("read saved program version: %w", err)
	}
	var info buildinfo.Info
	if json.Unmarshal(out, &info) != nil || info.Version == "" || info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return Candidate{}, errors.New("invalid rollback program version response")
	}
	if source.Version != "" && source.Version != info.Version {
		return Candidate{}, errors.New("rollback program version differs from the enabled version")
	}
	return Candidate{Version: info.Version, SHA256: digest, Path: path}, nil
}
