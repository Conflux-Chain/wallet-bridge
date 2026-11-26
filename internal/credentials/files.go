package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// FileName is shared by the service and its upgrade backup.
const FileName = "client.key"

var ErrInvalid = errors.New("invalid credential file; automatic replacement is disabled")

// Ensure initializes the service credential only. Valid files survive retries and
// restarts; damaged or insecure files cause an error instead of key rotation.
func Ensure(dir string) error {
	return ensure(dir, rand.Reader)
}

func ensure(dir string, random io.Reader) error {
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return err
	}
	_, err := Read(dir)
	if errors.Is(err, os.ErrNotExist) {
		return create(dir, random)
	}
	return err
}

// Read loads the service credential without creating files.
func Read(dir string) (string, error) {
	if err := platform.CheckPrivateDir(dir); err != nil {
		return "", err
	}
	f, err := platform.OpenPrivateFile(filepath.Join(dir, FileName))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 66))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", FileName, err)
	}
	if len(data) != 65 || data[64] != '\n' {
		return "", fmt.Errorf("%s: %w", FileName, ErrInvalid)
	}
	decoded, err := hex.DecodeString(string(data[:64]))
	if err != nil || hex.EncodeToString(decoded) != string(data[:64]) {
		return "", fmt.Errorf("%s: %w", FileName, ErrInvalid)
	}
	return string(data[:64]), nil
}

func create(dir string, random io.Reader) (result error) {
	temp := filepath.Join(dir, "."+FileName+"-"+rand.Text())
	f, err := platform.CreatePrivateFile(temp)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err := os.Remove(temp); err != nil {
			result = errors.Join(result, fmt.Errorf("remove credential temporary file: %w", err))
		}
	}()
	var key [32]byte
	if _, err := io.ReadFull(random, key[:]); err != nil {
		return fmt.Errorf("generate %s: %w", FileName, err)
	}
	if _, err := f.WriteString(hex.EncodeToString(key[:]) + "\n"); err != nil {
		return fmt.Errorf("write %s: %w", FileName, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", FileName, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Link publishes a complete file atomically and fails if another process won.
	if err := os.Link(temp, filepath.Join(dir, FileName)); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("publish %s (requires hard-link support): %w", FileName, err)
	}
	_, err = Read(dir)
	return err
}
