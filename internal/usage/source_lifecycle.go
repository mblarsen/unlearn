package usage

import (
	"database/sql"

	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/state"
)

// trackedEvidenceForPath leaves explicit requests strict. An ordinary scan never
// retries an inactive source; a forced rescan can restore it without discarding
// previously derived evidence or silently upgrading coverage.
func trackedEvidenceForPath(db *sql.DB, path string, names []string, opts Options, explicit bool, scan historyScannerFunc) ([]history.Evidence, bool, bool, error) {
	inactive, err := state.HistorySourceInactive(db, path)
	if err != nil {
		return nil, false, false, err
	}
	if inactive && !explicit && !opts.RescanSources {
		evidence, err := state.LoadHistoryEvidence(db, path)
		return evidence, true, false, err
	}
	evidence, missing, err := evidenceForPath(db, path, names, opts, explicit, scan)
	if err != nil {
		return nil, false, false, err
	}
	changed, err := state.SetHistorySourceInactive(db, path, missing)
	return evidence, missing, missing && changed, err
}
