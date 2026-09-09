package usage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestLoadMergesBestEvidenceAndAttachesToSkills(t *testing.T) {
	jsonlPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"timestamp":"2026-01-02T03:04:05Z","message":"alpha was mentioned"}`+"\n"+`{"message":"read skills/beta/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlitePath := writeUsageSQLite(t, t.TempDir(), "use the alpha skill")

	result, err := Load(Options{
		Config:          config.Default(),
		Paths:           testStatePaths(t),
		Skills:          []inventory.Skill{{Name: "Alpha"}, {Name: "beta"}},
		HistoryJSONL:    []string{jsonlPath},
		HistorySQLite:   []string{sqlitePath},
		HistoryCacheTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Evidence["alpha"]; got != string(history.EvidenceStrong) {
		t.Fatalf("alpha evidence=%q", got)
	}
	if got := result.Evidence["beta"]; got != string(history.EvidenceStrong) {
		t.Fatalf("beta evidence=%q", got)
	}
	if len(result.Sources["alpha"]) != 2 {
		t.Fatalf("alpha sources=%v", result.Sources["alpha"])
	}
	if len(result.Skills) != 2 || result.Skills[0].HistoryEvidence != "strong" || result.Skills[1].HistoryEvidence != "strong" {
		t.Fatalf("skills were not enriched: %#v", result.Skills)
	}
	if result.Skills[0].HistoryLastSeenAt.IsZero() {
		t.Fatalf("alpha last-seen timestamp was not attached")
	}
}

func TestLoadSkipsMissingConfiguredJSONLAndScansSurvivor(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "deleted-session.jsonl")
	survivingPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(survivingPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HistoryScan = true
	cfg.HistoryJSONL = []string{missingPath, survivingPath}

	result, err := Load(Options{
		Config:          cfg,
		Paths:           testStatePaths(t),
		Skills:          []inventory.Skill{{Name: "alpha"}},
		HistoryCacheTTL: time.Hour,
		RescanSources:   true,
	})
	if err != nil {
		t.Fatalf("configured history should tolerate a deleted source: %v", err)
	}
	if result.Evidence["alpha"] != "strong" {
		t.Fatalf("surviving source was not scanned: %v", result.Evidence)
	}
}

func TestLoadConfiguredMissingSources(t *testing.T) {
	for _, rescan := range []bool{false, true} {
		t.Run(fmt.Sprintf("rescan=%t", rescan), func(t *testing.T) {
			dir := t.TempDir()
			jsonlPath := filepath.Join(dir, "deleted-session.jsonl")
			sqlitePath := filepath.Join(dir, "deleted-session.db")
			cfg := config.Default()
			cfg.HistoryScan = true
			cfg.HistoryJSONL = []string{jsonlPath}
			cfg.HistorySQLite = []string{sqlitePath}

			result, err := Load(Options{
				Config:          cfg,
				Paths:           testStatePaths(t),
				Skills:          []inventory.Skill{{Name: "alpha"}},
				HistoryCacheTTL: time.Hour,
				RescanSources:   rescan,
			})
			if err != nil {
				t.Fatalf("configured history should tolerate deleted sources: %v", err)
			}
			if !reflect.DeepEqual(result.MissingSources, []string{jsonlPath, sqlitePath}) {
				t.Fatalf("missing sources=%v", result.MissingSources)
			}
			if len(result.Evidence) != 0 {
				t.Fatalf("unexpected evidence from missing sources: %v", result.Evidence)
			}
		})
	}
}

func TestLoadToleratesDiscoveredSQLiteRemovalBeforeScan(t *testing.T) {
	root := t.TempDir()
	sqlitePath := writeUsageSQLite(t, filepath.Join(root, "history"), "use the alpha skill")
	cfg := config.Default()
	cfg.HistoryScan = true
	removed := false

	result, err := Load(Options{
		Config:          cfg,
		Paths:           testStatePaths(t),
		Skills:          []inventory.Skill{{Name: "alpha"}},
		TrustedRoots:    []string{root},
		HistoryCacheTTL: time.Hour,
		Progress: func(progress Progress) {
			if !removed && progress.Step == "history" && !progress.Done {
				removed = true
				if err := os.Remove(sqlitePath); err != nil {
					t.Fatal(err)
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("discovery-to-scan removal should be tolerated: %v", err)
	}
	if !removed || !reflect.DeepEqual(result.MissingSources, []string{sqlitePath}) {
		t.Fatalf("race was not exercised: removed=%t missing=%v", removed, result.MissingSources)
	}
}

func TestLoadMissingConfiguredSourceUsesCacheUnlessForced(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := testStatePaths(t)
	skills := []inventory.Skill{{Name: "alpha"}}
	if _, err := Load(Options{Config: config.Default(), Paths: paths, Skills: skills, HistoryJSONL: []string{jsonlPath}, HistoryCacheTTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(jsonlPath); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HistoryScan = true
	cfg.HistoryJSONL = []string{jsonlPath}

	cached, err := Load(Options{Config: cfg, Paths: paths, Skills: skills, HistoryCacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if cached.Evidence["alpha"] != "strong" || cached.Skills[0].HistoryEvidence != "strong" {
		t.Fatalf("ordinary load should retain derived cached evidence: %#v", cached)
	}
	if !reflect.DeepEqual(cached.MissingSources, []string{jsonlPath}) {
		t.Fatalf("cached missing sources=%v", cached.MissingSources)
	}

	forced, err := Load(Options{Config: cfg, Paths: paths, Skills: skills, HistoryCacheTTL: time.Hour, RescanSources: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(forced.Evidence) != 0 {
		t.Fatalf("forced rescan must not reuse cached evidence: %v", forced.Evidence)
	}
	if !reflect.DeepEqual(forced.MissingSources, []string{jsonlPath}) {
		t.Fatalf("forced missing sources=%v", forced.MissingSources)
	}
	restarted, err := Load(Options{Config: cfg, Paths: paths, Skills: skills, HistoryCacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Evidence["alpha"] != "strong" || len(restarted.StoppedSources) != 0 || len(restarted.MissingSources) != 1 {
		t.Fatalf("inactive evidence lost or warning repeated after forced scan: %+v", restarted)
	}
}

func TestLoadMissingExplicitSourceStillFails(t *testing.T) {
	for _, source := range []struct {
		name string
		set  func(*Options, string)
	}{
		{name: "jsonl", set: func(opts *Options, path string) { opts.HistoryJSONL = []string{path} }},
		{name: "sqlite", set: func(opts *Options, path string) { opts.HistorySQLite = []string{path} }},
	} {
		t.Run(source.name, func(t *testing.T) {
			missingPath := filepath.Join(t.TempDir(), "explicit-missing."+source.name)
			opts := Options{Config: config.Default(), Paths: testStatePaths(t), Skills: []inventory.Skill{{Name: "alpha"}}, HistoryCacheTTL: time.Hour}
			source.set(&opts, missingPath)
			_, err := Load(opts)
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("explicit missing source error=%v", err)
			}
		})
	}
}

func TestLoadConfiguredSourceDoesNotMaskNonMissingErrors(t *testing.T) {
	t.Run("wrong JSONL file type", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.Default()
		cfg.HistoryScan = true
		cfg.HistoryJSONL = []string{dir}

		_, err := Load(Options{Config: cfg, Paths: testStatePaths(t), Skills: []inventory.Skill{{Name: "alpha"}}, HistoryCacheTTL: time.Hour})
		if err == nil {
			t.Fatal("configured directory passed as JSONL source should fail")
		}
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("non-missing source error was misclassified: %v", err)
		}
	})

	t.Run("corrupt SQLite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		if err := os.WriteFile(path, []byte("not a SQLite database"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.Default()
		cfg.HistoryScan = true
		cfg.HistorySQLite = []string{path}

		_, err := Load(Options{Config: cfg, Paths: testStatePaths(t), Skills: []inventory.Skill{{Name: "alpha"}}, HistoryCacheTTL: time.Hour})
		if err == nil {
			t.Fatal("corrupt configured SQLite source should fail")
		}
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("corrupt source error was misclassified: %v", err)
		}
	})
}

func TestLoadUsesFreshCachedEvidenceWithoutRawRescan(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := testStatePaths(t)
	opts := Options{Config: config.Default(), Paths: paths, Skills: []inventory.Skill{{Name: "alpha"}}, HistoryJSONL: []string{jsonlPath}, HistoryCacheTTL: time.Hour}
	first, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Evidence["alpha"] != "strong" {
		t.Fatalf("first evidence=%v", first.Evidence)
	}
	if err := os.WriteFile(jsonlPath, []byte(`{"message":"no skill invocation here"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(jsonlPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Evidence["alpha"] != "strong" {
		t.Fatalf("expected cached evidence, got %v", second.Evidence)
	}
}

func TestLoadDiscoversSQLiteOnlyWhenHistoryScanEnabled(t *testing.T) {
	root := t.TempDir()
	_ = writeUsageSQLite(t, filepath.Join(root, "history"), "use the alpha skill")
	paths := testStatePaths(t)
	skills := []inventory.Skill{{Name: "alpha"}}

	disabled, err := Load(Options{Config: config.Default(), Paths: paths, Skills: skills, TrustedRoots: []string{root}, HistoryCacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Evidence != nil {
		t.Fatalf("expected no discovery without opt-in, got %v", disabled.Evidence)
	}

	cfg := config.Default()
	cfg.HistoryScan = true
	enabled, err := Load(Options{Config: cfg, Paths: paths, Skills: skills, TrustedRoots: []string{root}, HistoryCacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Evidence["alpha"] != "strong" {
		t.Fatalf("expected discovered SQLite evidence, got %v", enabled.Evidence)
	}
}

func writeUsageSQLite(t *testing.T, dir, message string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (message TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (message) VALUES (?)`, message); err != nil {
		t.Fatal(err)
	}
	return path
}

func testStatePaths(t *testing.T) state.Paths {
	t.Helper()
	base := t.TempDir()
	return state.Paths{
		BaseDir:       base,
		ConfigPath:    filepath.Join(base, "config.toml"),
		IndexPath:     filepath.Join(base, "index.db"),
		QuarantineDir: filepath.Join(base, "quarantine"),
		LLMCacheDir:   filepath.Join(base, "llm-cache"),
	}
}
