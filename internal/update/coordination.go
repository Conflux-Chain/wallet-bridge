package update

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

type UpgradeBlock string

const (
	UpgradeInProgress       UpgradeBlock = "upgrade_in_progress"
	UpgradeRecoveryRequired UpgradeBlock = "upgrade_recovery_required"
)

func (b UpgradeBlock) Error() string {
	if b == UpgradeInProgress {
		return "An upgrade is in progress; retry after it finishes"
	}
	return "The upgrade was interrupted; run wallet-bridge upgrade to resume"
}

const (
	upgradeRecordName = "upgrade.json"
	upgradeLockDir    = "upgrade-lock"
	phasePreparing    = "preparing"
	phaseStopped      = "stopped"
	phaseVerifying    = "verifying"
	phaseAdmitted     = "admitted"
	phaseRestoring    = "restoring"
	phaseDataRestored = "data_restored"
	phaseRolledBack   = "rolled_back"
)

type upgradeRecord struct {
	ID       string           `json:"id"`
	Phase    string           `json:"phase"`
	Previous releaseSelection `json:"previous"`
	Target   releaseSelection `json:"target"`
	Failure  string           `json:"failure,omitempty"`
}

func (r upgradeRecord) valid() bool {
	if !upgradeIDPattern.MatchString(r.ID) || r.Previous.Version == "" || !validSHA256(r.Previous.SHA256) ||
		!ValidReleaseTag(r.Target.Version) || !validSHA256(r.Target.SHA256) {
		return false
	}
	switch r.Phase {
	case phasePreparing, phaseStopped, phaseVerifying, phaseAdmitted:
		return r.Failure == ""
	case phaseRestoring, phaseDataRestored, phaseRolledBack:
		return r.Failure != ""
	default:
		return false
	}
}

func (r upgradeRecord) committed() bool {
	return r.Phase == phaseAdmitted || r.Phase == phaseRolledBack
}

func (r upgradeRecord) selection() activeSelection {
	if r.Failure != "" {
		return activeSelection{releaseSelection: r.Previous, Retained: true}
	}
	return activeSelection{releaseSelection: r.Target}
}

// Reserve room for the escaped failure diagnostic when an existing upgrade
// enters recovery. Older records fit within the original 4 KiB limit.
const maxUpgradeRecord = 16 << 10

func readUpgradeRecord(dir string) (*upgradeRecord, error) {
	f, err := platform.OpenPrivateRecord(filepath.Join(dir, upgradeRecordName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUpgradeRecord+1))
	if err != nil {
		return nil, err
	}
	var record upgradeRecord
	if len(data) > maxUpgradeRecord || json.Unmarshal(data, &record) != nil || !record.valid() {
		return nil, errors.New("invalid upgrade record")
	}
	return &record, nil
}

// CheckUpgrade inspects ordinary access without creating a lock or data
// directory. The service repeats the check under its own lock before opening SQLite.
func CheckUpgrade(dir string) (UpgradeBlock, error) {
	if err := platform.CheckPrivateDir(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	lock, err := platform.ObserveServiceLock(filepath.Join(dir, upgradeLockDir))
	if errors.Is(err, platform.ErrLocked) {
		return UpgradeInProgress, nil
	}
	if err != nil {
		return "", err
	}
	if lock != nil {
		defer lock.Close()
	}
	record, err := readUpgradeRecord(dir)
	if err != nil || record == nil {
		return "", err
	}
	if record.committed() {
		if err := checkAdmittedSelection(dir, record.selection()); err == nil {
			return "", nil
		}
	}
	return UpgradeRecoveryRequired, nil
}

type Upgrade struct {
	dir     string
	current *upgradeRecord
}

// The command holds this kernel lock across the whole switch, including
// service restarts. A new owner reloads the last published phase.
func WithUpgrade(ctx context.Context, dir string, run func(*Upgrade) error) (err error) {
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return err
	}
	lock, err := acquireUpgradeLock(ctx, filepath.Join(dir, upgradeLockDir))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	current, err := readUpgradeRecord(dir)
	if err != nil {
		return err
	}
	return run(&Upgrade{dir: dir, current: current})
}

func acquireUpgradeLock(ctx context.Context, dir string) (*platform.ServiceLock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := platform.AcquireServiceLock(dir)
		if !errors.Is(err, platform.ErrLocked) {
			return lock, err
		}
		// A status reader may briefly hold a shared lock. Only an exclusive
		// owner is a competing upgrade.
		busy, err := platform.ServiceLocked(dir)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, UpgradeInProgress
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (w *Upgrade) begin(id string, previous, target releaseSelection) error {
	if w.current != nil {
		return UpgradeRecoveryRequired
	}
	return w.save(upgradeRecord{ID: id, Phase: phasePreparing, Previous: previous, Target: target})
}

func (w *Upgrade) advance(next string) error {
	if w.current == nil {
		return errors.New("no upgrade to advance")
	}
	record := *w.current
	if record.Phase == next {
		return nil
	}
	if !(record.Phase == phasePreparing && next == phaseStopped || record.Phase == phaseStopped && next == phaseVerifying ||
		record.Phase == phaseVerifying && next == phaseAdmitted || record.Phase == phaseRestoring && next == phaseDataRestored ||
		record.Phase == phaseDataRestored && next == phaseRolledBack) {
		return fmt.Errorf("invalid upgrade transition from %s to %s", record.Phase, next)
	}
	record.Phase = next
	return w.save(record)
}

func (w *Upgrade) save(record upgradeRecord) error {
	data, err := json.Marshal(record)
	if err != nil || len(data) > maxUpgradeRecord || (record.Failure == "" && len(data) > 4096) || !record.valid() {
		return errors.New("invalid upgrade record")
	}
	temp := filepath.Join(w.dir, ".upgrade.next")
	if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writeBackupFile(temp, string(data)); err != nil {
		return err
	}
	if err := platform.CommitRecord(temp, filepath.Join(w.dir, upgradeRecordName)); err != nil {
		return err
	}
	w.current = &record
	return nil
}
