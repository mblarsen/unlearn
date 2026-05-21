package usage

import (
	"database/sql"
	"os"
	"path/filepath"
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
