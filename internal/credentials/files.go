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

type Kind uint8

const (
	Client Kind = iota
	Control
)

var ErrInvalid = errors.New("invalid credential file; automatic replacement is disabled")

func filename(kind Kind) (string, error) {
	switch kind {
	case Client:
		return "client.key", nil
	case Control:
		return "control.key", nil
	default:
		return "", errors.New("unknown credential kind")
	}
}

// Ensure initializes missing credentials only. Valid files survive retries and
// restarts; damaged or insecure files cause an error instead of key rotation.
func Ensure(dir string) error {
	return ensure(dir, rand.Reader)
}

func ensure(dir string, random io.Reader) error {
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return err
	}
	var missing []Kind
	for _, kind := range []Kind{Client, Control} {
		if _, err := Read(dir, kind); errors.Is(err, os.ErrNotExist) {
			missing = append(missing, kind)
		} else if err != nil {
			return err
		}
	}
	for _, kind := range missing {
		if err := create(dir, kind, random); err != nil {
			return err
		}
	}
	return nil
}

// Read loads only the requested credential, without creating files.
func Read(dir string, kind Kind) (string, error) {
	name, err := filename(kind)
	if err != nil {
		return "", err
	}
	if err := platform.CheckPrivateDir(dir); err != nil {
		return "", err
	}
	f, err := platform.OpenPrivateFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 66))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) != 65 || data[64] != '\n' {
		return "", fmt.Errorf("%s: %w", name, ErrInvalid)
	}
	decoded, err := hex.DecodeString(string(data[:64]))
	if err != nil || hex.EncodeToString(decoded) != string(data[:64]) {
		return "", fmt.Errorf("%s: %w", name, ErrInvalid)
	}
	return string(data[:64]), nil
}

func create(dir string, kind Kind, random io.Reader) (result error) {
	name, err := filename(kind)
	if err != nil {
		return err
	}
	temp := filepath.Join(dir, "."+name+"-"+rand.Text())
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
		return fmt.Errorf("generate %s: %w", name, err)
	}
	if _, err := f.WriteString(hex.EncodeToString(key[:]) + "\n"); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Link publishes a complete file atomically and fails if another process won.
	if err := os.Link(temp, filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("publish %s (requires hard-link support): %w", name, err)
	}
	_, err = Read(dir, kind)
	return err
}
