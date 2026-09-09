// Package workbench executes authorized skill mutations and reconciles the
// resulting filesystem state with one in-memory and persisted inventory snapshot.
package workbench

import (
	"errors"
	"fmt"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/state"
)

// Kind identifies a durable workbench mutation.
type Kind string

const (
	Quarantine Kind = "quarantine"
	Delete     Kind = "delete"
	Rename     Kind = "rename"
	Restore    Kind = "restore"
)

// Phase identifies which non-atomic part of a mutation failed.
type Phase string

const (
	AuthorizationPhase  Phase = "authorization"
	FilesystemPhase     Phase = "filesystem"
	ReconciliationPhase Phase = "reconciliation"
	PersistencePhase    Phase = "persistence"
)

var ErrAuthorizationRequired = errors.New("explicit mutation authorization required")

// Request contains the caller-confirmed operation and the snapshot it applies to.
type Request struct {
	Kind            Kind
	Authorized      bool
	Snapshot        inventorysnapshot.Snapshot
	Targets         []inventory.Skill
	Confirmation    fsactions.DeleteConfirmation
	NewName         string
	RestoreName     string
	DestinationRoot string
}

// Failure preserves whether filesystem mutation, reconciliation, or persistence
// failed; callers can therefore recover instead of assuming an impossible rollback.
type Failure struct {
	Phase Phase
	Err   error
}

func (f Failure) Error() string { return fmt.Sprintf("%s: %v", f.Phase, f.Err) }
func (f Failure) Unwrap() error { return f.Err }

// Outcome is the complete observable mutation result.
type Outcome struct {
	Snapshot         inventorysnapshot.Snapshot
	Removed          []inventory.Skill
	Missing          []inventory.Skill
	Paths            []string
	Renamed          *inventory.Skill
	Restored         *inventory.Skill
	RenamePreview    fsactions.RenamePreview
	Failures         []Failure
	RecoveryRequired bool
}

// Err joins all phase-specific failures.
func (o Outcome) Err() error {
	if len(o.Failures) == 0 {
		return nil
	}
	errs := make([]error, 0, len(o.Failures))
	for _, failure := range o.Failures {
		errs = append(errs, failure)
	}
	return errors.Join(errs...)
}

// HasFailure reports whether a specific mutation phase failed.
func (o Outcome) HasFailure(phase Phase) bool {
	for _, failure := range o.Failures {
		if failure.Phase == phase {
			return true
		}
	}
	return false
}

// Module hides filesystem execution, exact identity, snapshot reconciliation,
// and SQLite persistence behind one operation interface.
type Module struct {
	Config        config.Config
	IndexPath     string
	QuarantineDir string

	persist func(inventorysnapshot.Snapshot) error
}

// Execute performs an explicitly authorized operation. Filesystem and SQLite
// cannot be atomic: when persistence fails, Snapshot still describes the known
// filesystem result and RecoveryRequired tells callers to persist/rescan it.
func (m Module) Execute(request Request) Outcome {
	outcome := Outcome{Snapshot: request.Snapshot.Clone()}
	if !request.Authorized {
		outcome.Failures = append(outcome.Failures, Failure{Phase: AuthorizationPhase, Err: ErrAuthorizationRequired})
		return outcome
	}

	manager := fsactions.Manager{Config: m.Config, QuarantineDir: m.QuarantineDir}
	changed := false
	switch request.Kind {
	case Quarantine:
		result, err := manager.QuarantineSelected(request.Targets, true)
		changed = m.applyRemovalResult(&outcome, result)
		outcome.addFailure(FilesystemPhase, err)
	case Delete:
		result, err := manager.DeleteSelected(request.Targets, request.Confirmation)
		changed = m.applyRemovalResult(&outcome, result)
		outcome.addFailure(FilesystemPhase, err)
	case Rename:
		if len(request.Targets) != 1 {
			outcome.addFailure(FilesystemPhase, fmt.Errorf("rename requires one exact install"))
			break
		}
		preview, err := fsactions.Rename(request.Targets[0], request.NewName, m.Config, true)
		outcome.RenamePreview = preview
		if err != nil {
			outcome.addFailure(FilesystemPhase, err)
			break
		}
		renamedSnapshot, renamed := inventorysnapshot.Rename(outcome.Snapshot, request.Targets[0], preview.NewName, preview.NewPath)
		outcome.Snapshot = renamedSnapshot
		outcome.Renamed = &renamed
		changed = true
	case Restore:
		dest, err := manager.Restore(request.RestoreName, request.DestinationRoot)
		if err != nil {
			outcome.addFailure(FilesystemPhase, err)
			break
		}
		outcome.Paths = []string{dest}
		restored, scanErr := scanExactInstall(request.DestinationRoot, dest)
		if scanErr != nil {
			outcome.addFailure(ReconciliationPhase, scanErr)
			outcome.RecoveryRequired = true
			break
		}
		outcome.Snapshot = inventorysnapshot.Add(outcome.Snapshot, restored)
		outcome.Restored = &restored
		changed = true
	default:
		outcome.addFailure(FilesystemPhase, fmt.Errorf("unknown mutation kind %q", request.Kind))
	}

	if changed {
		if err := m.persistSnapshot(outcome.Snapshot); err != nil {
			outcome.addFailure(PersistencePhase, err)
			outcome.RecoveryRequired = true
		}
	}
	return outcome
}

func (m Module) applyRemovalResult(outcome *Outcome, result fsactions.Result) bool {
	outcome.Removed = append([]inventory.Skill(nil), result.Skills...)
	outcome.Missing = append([]inventory.Skill(nil), result.Missing...)
	outcome.Paths = append([]string(nil), result.Paths...)
	if len(result.Skills) == 0 {
		return false
	}
	outcome.Snapshot = inventorysnapshot.Remove(outcome.Snapshot, result.Skills)
	return true
}

func (m Module) persistSnapshot(snapshot inventorysnapshot.Snapshot) error {
	if m.persist != nil {
		return m.persist(snapshot)
	}
	if m.IndexPath == "" {
		return nil
	}
	db, err := state.OpenIndex(m.IndexPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return state.ReplaceIndex(db, snapshot.Skills, snapshot.Findings)
}

func (o *Outcome) addFailure(phase Phase, err error) {
	if err != nil {
		o.Failures = append(o.Failures, Failure{Phase: phase, Err: err})
	}
}

func scanExactInstall(root, path string) (inventory.Skill, error) {
	report, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: []string{root}})
	if err != nil {
		return inventory.Skill{}, err
	}
	target := inventory.Skill{EncounteredPath: path}
	for _, skill := range report.Skills {
		if inventorysnapshot.SameInstall(skill, target) {
			return skill, nil
		}
	}
	return inventory.Skill{}, fmt.Errorf("restored install %s was not found during reconciliation", path)
}
