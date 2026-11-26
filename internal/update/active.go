package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// ServiceProgram resolves the executable for a new service in its private data directory.
func ServiceProgram(dir string) (Candidate, error) {
	if err := platform.CheckPrivateDir(dir); err != nil {
		return Candidate{}, err
	}
	active := filepath.Join(dir, "active")
	if err := platform.CheckPrivateDir(active); err == nil {
		return readSelection(dir, active)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Candidate{}, err
	}
	path := installedPath(dir)
	if err := platform.CheckPrivateDir(filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		exe, err := os.Executable()
		return Candidate{Path: exe}, err
	} else if err != nil {
		return Candidate{}, err
	}
	digest, err := executableDigest(path)
	return Candidate{Path: path, SHA256: digest}, err
}

func readSelection(dir, active string) (Candidate, error) {
	f, err := platform.OpenPrivateFile(filepath.Join(active, "selection.json"))
	if err != nil {
		return Candidate{}, err
	}
	defer f.Close()
	const maxSize = 4096
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return Candidate{}, err
	}
	var state struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	if len(data) > maxSize || json.Unmarshal(data, &state) != nil || !ValidReleaseTag(state.Version) || !validSHA256(state.SHA256) {
		return Candidate{}, errors.New("invalid active version record")
	}
	versions := filepath.Join(dir, "versions")
	target := filepath.Join(versions, state.Version)
	for _, parent := range []string{versions, target} {
		if err := platform.CheckPrivateDir(parent); err != nil {
			return Candidate{}, err
		}
	}
	name, err := nativeAssetName()
	if err != nil {
		return Candidate{}, err
	}
	result := Candidate{Version: state.Version, Path: filepath.Join(target, name), SHA256: state.SHA256}
	return result, verifyExecutable(result.Path, result.SHA256)
}

// CheckServiceProgram is called under the service lock before opening its database.
func CheckServiceProgram(dir string) error {
	selected, err := ServiceProgram(dir)
	if err != nil || selected.SHA256 == "" {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()
	current, err := fileDigest(f)
	if err != nil {
		return err
	}
	if current != selected.SHA256 {
		return fmt.Errorf("start the enabled program instead: %s", selected.Path)
	}
	return nil
}
