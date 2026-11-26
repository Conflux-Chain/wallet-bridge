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

func nativeAssetName() (string, error) {
	name := "wallet-bridge-" + runtime.GOOS + "-" + runtime.GOARCH
	switch name {
	case "wallet-bridge-windows-amd64":
		name += ".exe"
	case "wallet-bridge-darwin-arm64", "wallet-bridge-darwin-amd64":
	default:
		return "", fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return name, nil
}

func stage(ctx context.Context, client *http.Client, tag, dir string) (Candidate, error) {
	name, err := nativeAssetName()
	if err != nil {
		return Candidate{}, err
	}
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
		if err := verifyExecutable(result.Path, result.SHA256); err == nil {
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
	if err := writeExecutable(temp, func(w io.Writer) error { return asset.copyVerified(ctx, client, w) }); err != nil {
		return Candidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if err := platform.EnsurePrivateDir(finalDir); err != nil {
		return Candidate{}, err
	}
	if err := publishExecutable(temp.Name(), result.Path, result.SHA256); err != nil {
		return Candidate{}, err
	}
	return result, nil
}

func writeExecutable(f *os.File, write func(io.Writer) error) (err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := write(f); err != nil {
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

// Link only a complete program. A concurrent installer may already have won.
func publishExecutable(temp, path, digest string) error {
	err := os.Link(temp, path)
	if errors.Is(err, os.ErrExist) {
		return verifyExecutable(path, digest)
	}
	return err
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

func verifyExecutable(path, digest string) error {
	actual, err := executableDigest(path)
	if err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("existing program checksum differs: %s", path)
	}
	return nil
}

func executableDigest(path string) (string, error) {
	f, err := platform.OpenPrivateExecutable(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := checkNativeExecutable(f); err != nil {
		return "", err
	}
	return fileDigest(f)
}

func fileDigest(f *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
