package update

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// Install copies the running program to the current user's fixed CLI and MCP entry.
func Install(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = platform.DataDir()
		if err != nil {
			return "", err
		}
	}
	bin := filepath.Join(dir, "bin")
	for _, parent := range []string{dir, bin} {
		if err := platform.EnsurePrivateDir(parent); err != nil {
			return "", err
		}
	}
	path := installedPath(dir)
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	source, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer source.Close()

	digest, err := fileDigest(source)
	if err != nil {
		return "", err
	}
	if err := verifyExecutable(path, digest); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	temp, err := platform.CreatePrivateFile(filepath.Join(bin, ".install-"+rand.Text()))
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	if err := writeExecutable(temp, func(w io.Writer) error {
		_, err := io.Copy(w, source)
		return err
	}); err != nil {
		return "", err
	}
	if err := publishExecutable(temp.Name(), path, digest); err != nil {
		return "", err
	}
	return path, nil
}

func installedPath(dir string) string {
	path := filepath.Join(dir, "bin", "wallet-bridge")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	return path
}
