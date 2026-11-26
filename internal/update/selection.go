package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// The sole upgrade writer publishes a fixed location and verified digest.
func (state activeSelection) publish(dir string) (err error) {
	if err := platform.CheckPrivateDir(dir); err != nil {
		return err
	}
	if _, err := state.program(dir); err != nil {
		return err
	}
	active := filepath.Join(dir, "active")
	err = platform.CheckPrivateDir(active)
	first := errors.Is(err, os.ErrNotExist)
	if err != nil && !first {
		return err
	}

	// The fixed staging location is reusable after a process exit. No reader
	// consults it, including when the first active record is being prepared.
	staging := filepath.Join(dir, "active.next")
	if err := platform.EnsurePrivateDir(staging); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(staging)) }()
	record := filepath.Join(staging, selectionFile)
	if err := os.Remove(record); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := platform.CreatePrivateFile(record)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(state)
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	// Root.Rename uses handle-relative rename on Windows, including support
	// for replacing a record still open by a reader sharing delete access.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if first {
		return root.Rename("active.next", "active")
	}
	return root.Rename(filepath.Join("active.next", selectionFile), filepath.Join("active", selectionFile))
}
