package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

type Candidate struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

func Stage(ctx context.Context, tag, dir string) (Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return stage(ctx, &http.Client{CheckRedirect: assetRedirect}, tag, dir)
}

func stage(ctx context.Context, client *http.Client, tag, dir string) (Candidate, error) {
	name := "wallet-bridge-" + runtime.GOOS + "-" + runtime.GOARCH
	switch name {
	case "wallet-bridge-windows-amd64":
		name += ".exe"
	case "wallet-bridge-darwin-arm64", "wallet-bridge-darwin-amd64":
	default:
		return Candidate{}, fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	var err error
	if dir == "" {
		dir, err = platform.DataDir()
		if err != nil {
			return Candidate{}, err
		}
	}
	if !filepath.IsAbs(dir) {
		return Candidate{}, errors.New("data directory must be absolute")
	}
	asset, err := resolveProgram(ctx, client, tag, name)
	if err != nil {
		return Candidate{}, err
	}
	versions := filepath.Join(dir, "versions")
	for _, parent := range []string{dir, versions} {
		if err := platform.EnsurePrivateDir(parent); err != nil {
			return Candidate{}, err
		}
	}
	finalDir := filepath.Join(versions, tag)
	result := Candidate{Version: tag, Path: filepath.Join(finalDir, name), SHA256: strings.TrimPrefix(asset.Digest, "sha256:")}
	if err := platform.CheckPrivateDir(finalDir); err == nil {
		if err := verifyExisting(result); err == nil {
			return result, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return Candidate{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Candidate{}, err
	}

	temp, err := platform.CreatePrivateFile(filepath.Join(versions, ".download-"+rand.Text()))
	if err != nil {
		return Candidate{}, err
	}
	defer os.Remove(temp.Name())
	if err := writeCandidate(ctx, client, asset, temp); err != nil {
		return Candidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if err := platform.EnsurePrivateDir(finalDir); err != nil {
		return Candidate{}, err
	}
	// Competing downloads cannot overwrite an already complete candidate.
	if err := os.Link(temp.Name(), result.Path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Candidate{}, err
		}
		if err := verifyExisting(result); err != nil {
			return Candidate{}, err
		}
	}
	return result, nil
}

func writeCandidate(ctx context.Context, client *http.Client, asset releaseAsset, f *os.File) (err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := asset.copyVerified(ctx, client, f); err != nil {
		return err
	}
	if err := checkNativeExecutable(f); err != nil {
		return err
	}
	if err := f.Chmod(0700); err != nil {
		return err
	}
	return f.Sync()
}

func checkNativeExecutable(f *os.File) error {
	switch runtime.GOOS {
	case "windows":
		program, err := pe.NewFile(f)
		if err == nil && program.Machine == pe.IMAGE_FILE_MACHINE_AMD64 &&
			program.Characteristics&(pe.IMAGE_FILE_EXECUTABLE_IMAGE|pe.IMAGE_FILE_DLL) == pe.IMAGE_FILE_EXECUTABLE_IMAGE {
			return nil
		}
	case "darwin":
		program, err := macho.NewFile(f)
		if err == nil && program.Type == macho.TypeExec &&
			((runtime.GOARCH == "amd64" && program.Cpu == macho.CpuAmd64) || (runtime.GOARCH == "arm64" && program.Cpu == macho.CpuArm64)) {
			return nil
		}
	}
	return fmt.Errorf("candidate is not a %s/%s executable", runtime.GOOS, runtime.GOARCH)
}

func verifyExisting(c Candidate) error {
	info, err := os.Lstat(c.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid candidate file: %s", c.Path)
	}
	f, err := os.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errors.New("candidate changed while opening")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != c.SHA256 {
		return fmt.Errorf("existing candidate checksum differs: %s", c.Path)
	}
	if runtime.GOOS == "darwin" && info.Mode().Perm() != 0700 {
		return errors.New("unexpected candidate permissions")
	}
	return checkNativeExecutable(f)
}
