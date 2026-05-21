package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/state"
)

// Progress describes usage-evidence orchestration progress across selected
// history sources. Adapter-level line/row progress is reported separately via
// history.ScanProgress.
type Progress struct {
	Step    string
	Current int
	Total   int
	Detail  string
	Done    bool
}

// Options controls opt-in, derived-only history evidence loading.
type Options struct {
	Config       config.Config
	Paths        state.Paths
	Skills       []inventory.Skill
	TrustedRoots []string

	HistoryJSONL    []string
	HistorySQLite   []string
	HistoryCacheTTL time.Duration
	RescanSources   bool

	Context         context.Context
	Progress        func(Progress)
	HistoryProgress func(history.ScanProgress)
}

// Result contains derived usage evidence keyed by normalized skill name and an
// enriched skill slice. It intentionally contains only grades, source paths, and
// last-seen timestamps; raw history excerpts are neither exposed nor persisted.
type Result struct {
	Evidence analysis.UsageEvidence
	Sources  map[string][]string
	LastSeen map[string]time.Time
	Skills   []inventory.Skill
}

// DiscoverPiJSONL returns likely Pi JSONL history files without reading their
// contents. Callers still need explicit history-scan opt-in before loading them.
func DiscoverPiJSONL() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return history.DiscoverPiJSONL(home, history.DefaultDiscoveryLimit)
}

// DiscoverSQLite returns likely SQLite history files below trusted scan roots
// without opening or reading their contents.
func DiscoverSQLite(roots []string) ([]string, error) {
	return history.DiscoverSQLite(roots, history.DefaultDiscoveryLimit)
}

// Load selects configured or CLI-provided history sources, scans or reuses
// cached derived evidence, merges the best evidence per skill, and attaches the
// result to inventory skills.
func Load(opts Options) (Result, error) {
	jsonlPaths, sqlitePaths, err := selectedSources(opts)
	if err != nil {
		return Result{}, err
	}
	if len(jsonlPaths) == 0 && len(sqlitePaths) == 0 {
		return Result{}, nil
	}
	if err := opts.Paths.Ensure(); err != nil {
		return Result{}, err
	}
	db, err := state.OpenIndex(opts.Paths.IndexPath)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()

	names := skillNames(opts.Skills)
	result := Result{Evidence: analysis.UsageEvidence{}, Sources: map[string][]string{}, LastSeen: map[string]time.Time{}}
	merge := func(evidence []history.Evidence) {
		mergeEvidence(result.Evidence, result.Sources, result.LastSeen, evidence)
	}

	jsonlAdapter := history.JSONLAdapter{}
	total := len(jsonlPaths) + len(sqlitePaths)
	for index, path := range jsonlPaths {
		reportProgress(opts.Progress, Progress{Step: "history", Current: index + 1, Total: total, Detail: filepath.Base(path)})
		evidence, err := evidenceForPath(db, path, names, opts, func(path string, names []string, scanOpts history.ScanOptions) ([]history.Evidence, error) {
			return jsonlAdapter.ScanWithOptions(path, names, scanOpts)
		})
		if err != nil {
			return Result{}, err
		}
		merge(evidence)
	}

	sqliteAdapter := history.SQLiteAdapter{}
	for index, path := range sqlitePaths {
		reportProgress(opts.Progress, Progress{Step: "history", Current: len(jsonlPaths) + index + 1, Total: total, Detail: filepath.Base(path)})
		evidence, err := evidenceForPath(db, path, names, opts, func(path string, names []string, scanOpts history.ScanOptions) ([]history.Evidence, error) {
			return sqliteAdapter.ScanWithOptions(path, names, scanOpts)
		})
		if err != nil {
			return Result{}, err
		}
		merge(evidence)
	}
	reportProgress(opts.Progress, Progress{Step: "history", Detail: fmt.Sprintf("%d file(s), %d matching skills", total, len(result.Evidence)), Done: true})
	result.Skills = Attach(opts.Skills, result.Evidence, result.Sources, result.LastSeen)
	return result, nil
}

func reportProgress(progress func(Progress), event Progress) {
	if progress != nil {
		progress(event)
	}
}

func selectedSources(opts Options) ([]string, []string, error) {
	jsonlPaths := append([]string(nil), opts.HistoryJSONL...)
	sqlitePaths := append([]string(nil), opts.HistorySQLite...)
	if len(jsonlPaths) == 0 && len(sqlitePaths) == 0 && opts.Config.HistoryScan {
		jsonlPaths = append([]string(nil), opts.Config.HistoryJSONL...)
		sqlitePaths = append([]string(nil), opts.Config.HistorySQLite...)
	}
	if opts.Config.HistoryScan && len(opts.HistoryJSONL) == 0 && len(opts.HistorySQLite) == 0 {
		discoveredSQLite, err := DiscoverSQLite(opts.TrustedRoots)
		if err != nil {
			return nil, nil, err
		}
		for _, path := range discoveredSQLite {
			sqlitePaths = appendUnique(sqlitePaths, path)
		}
	}
	return jsonlPaths, sqlitePaths, nil
}

func skillNames(skills []inventory.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

type historyScannerFunc func(path string, names []string, opts history.ScanOptions) ([]history.Evidence, error)

func evidenceForPath(db *sql.DB, path string, names []string, opts Options, scan historyScannerFunc) ([]history.Evidence, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if !opts.RescanSources {
		status, err := state.HistoryCacheStatusForSource(db, path, info.ModTime(), opts.HistoryCacheTTL, now)
		if err != nil {
			return nil, err
		}
		if status.Fresh {
			return state.LoadHistoryEvidence(db, path)
		}
	}
	historyProgress := func(progress history.ScanProgress) {
		if opts.HistoryProgress != nil {
			opts.HistoryProgress(progress)
		}
	}
	evidence, err := scan(path, names, history.ScanOptions{Context: opts.Context, Progress: historyProgress})
	if err != nil {
		return nil, err
	}
	if err := state.SaveHistoryEvidence(db, path, info.ModTime(), evidence); err != nil {
		return nil, err
	}
	return evidence, nil
}

func mergeEvidence(usage analysis.UsageEvidence, sources map[string][]string, lastSeen map[string]time.Time, evidence []history.Evidence) {
	for _, item := range evidence {
		current := usage[item.SkillName]
		if current == "" || evidenceRank(item.Grade) < evidenceRank(history.EvidenceGrade(current)) {
			usage[item.SkillName] = string(item.Grade)
		}
		sources[item.SkillName] = appendUnique(sources[item.SkillName], item.Source)
		if item.SeenAt.After(lastSeen[item.SkillName]) {
			lastSeen[item.SkillName] = item.SeenAt
		}
	}
}

// Attach returns a copy of skills enriched with usage evidence.
func Attach(skills []inventory.Skill, evidence analysis.UsageEvidence, sources map[string][]string, lastSeen map[string]time.Time) []inventory.Skill {
	if evidence == nil {
		return skills
	}
	enriched := append([]inventory.Skill(nil), skills...)
	for i := range enriched {
		key := strings.ToLower(enriched[i].Name)
		enriched[i].HistoryEvidence = evidence[key]
		enriched[i].HistorySources = append([]string(nil), sources[key]...)
		enriched[i].HistoryLastSeenAt = lastSeen[key]
	}
	return enriched
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func evidenceRank(grade history.EvidenceGrade) int {
	switch grade {
	case history.EvidenceStrong:
		return 1
	case history.EvidenceMedium:
		return 2
	case history.EvidenceWeak:
		return 3
	default:
		return 99
	}
}
