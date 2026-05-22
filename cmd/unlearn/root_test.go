package unlearn

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestLoadingModelShowsProgress(t *testing.T) {
	updates := make(chan tea.Msg, 1)
	m := newLoadingModel(updates)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	m = updated.(loadingModel)
	updated, _ = m.Update(loadingProgressMsg{event: inventoryProgress{Step: "history", Detail: "/sessions/a.jsonl · 500 lines · 2 matching skills"}})
	m = updated.(loadingModel)

	view := m.View()
	if strings.Contains(view, "unlearn is loading") {
		t.Fatalf("loading view should not include redundant loading title:\n%s", view)
	}
	if !strings.Contains(view, "Scan history evidence") || !strings.Contains(view, "500 lines") {
		t.Fatalf("loading view missing progress details:\n%s", view)
	}
}

func TestDashboardInventoryUsesCachedIndex(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	db, err := state.OpenIndex(filepath.Join(stateDir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	cachedSkills := []inventory.Skill{{ID: "cached", Name: "cached", Root: "/missing-root", EncounteredPath: "/missing-root/cached", Kind: inventory.KindDirectory}}
	cachedFindings := []analysis.Finding{{ID: "tokens:cached", Type: analysis.FindingHighTokenCost, Severity: 3, Title: "cached", Skills: cachedSkills, Reasons: []string{"cached finding"}}}
	if err := state.ReplaceIndex(db, cachedSkills, cachedFindings); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	skills, findings, err := loadDashboardInventory(&cliOptions{stateDir: stateDir, configPath: configPath}, inventoryLoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "cached" || len(findings) != 1 || findings[0].ID != "tokens:cached" {
		t.Fatalf("dashboard did not load cached inventory: skills=%#v findings=%#v", skills, findings)
	}
}

func TestDashboardInventoryIgnoresLegacyDuplicateCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.SetupComplete = true
	cfg.ActiveAgents = []string{"pi"}
	cfg.TrustRoot(filepath.Join(home, ".agents", "skills"))
	cfg.TrustRoot(filepath.Join(home, ".pi", "agent", "skills"))
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}

	db, err := state.OpenIndex(filepath.Join(stateDir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	legacyPayload := `{"skills":[{"ID":"cached","Name":"find-skills","Root":"/stale","EncounteredPath":"/stale/find-skills","Kind":"directory"}],"findings":[{"ID":"duplicate:find-skills","Type":"duplicate","Severity":1,"Title":"find-skills","Skills":null,"Reasons":["legacy duplicate"]}]}`
	if _, err := db.Exec(`INSERT INTO inventory_cache(key, payload, updated_at) VALUES (?, ?, ?)`, "dashboard-inventory-v1", legacyPayload, "2026-05-22T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	skills, findings, err := loadDashboardInventory(&cliOptions{stateDir: stateDir, configPath: configPath}, inventoryLoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 || len(findings) != 0 {
		t.Fatalf("dashboard loaded legacy duplicate cache instead of rescanning: skills=%#v findings=%#v", skills, findings)
	}
}

func TestResetYesRemovesLocalStateButKeepsQuarantine(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	indexPath := filepath.Join(stateDir, "index.db")
	llmCachePath := filepath.Join(stateDir, "llm-cache", "summary.txt")
	quarantinePath := filepath.Join(stateDir, "quarantine", "2026-05-22", "demo", "SKILL.md")
	for _, path := range []string{llmCachePath, quarantinePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.SetupComplete = true
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	if db, err := state.OpenIndex(indexPath); err != nil {
		t.Fatal(err)
	} else if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"reset", "--yes", "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{configPath, indexPath, filepath.Join(stateDir, "llm-cache")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", path, err)
		}
	}
	if _, err := os.Stat(quarantinePath); err != nil {
		t.Fatalf("quarantine should be kept: %v", err)
	}
	got := out.String()
	for _, want := range []string{"remove SQLite index", "keep quarantine", "Reset complete"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reset output missing %q:\n%s", want, got)
		}
	}
}

func TestResetRequiresConfirmation(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.SetupComplete = true
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetIn(strings.NewReader("no\n"))
	cmd.SetArgs([]string{"reset", "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config should remain after cancelled reset: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Type yes to continue") || !strings.Contains(got, "Reset cancelled") {
		t.Fatalf("unexpected reset prompt output:\n%s", got)
	}
}

func TestResetLLMSummaryByContentHashRemovesSummaryAndOverlapCache(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cacheDir := filepath.Join(stateDir, "llm-cache")
	writeFile(t, llm.SummaryCachePath(cacheDir, "hash/one"), `{"summary":"cached"}`)
	writeFile(t, filepath.Join(llm.OverlapCacheDir(cacheDir), "overlap.json"), `{"overlaps":[]}`)

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"reset", "llm-summary", "hash/one", "--yes", "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(llm.SummaryCachePath(cacheDir, "hash/one")); !os.IsNotExist(err) {
		t.Fatalf("expected summary cache to be removed, err=%v", err)
	}
	if _, err := os.Stat(llm.OverlapCacheDir(cacheDir)); !os.IsNotExist(err) {
		t.Fatalf("expected overlap cache to be removed, err=%v", err)
	}
	got := out.String()
	for _, want := range []string{"hash/one", "Removed 1 cached LLM summary", "Removed overlap cache"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reset llm-summary output missing %q:\n%s", want, got)
		}
	}
}

func TestResetLLMSummaryBySkillNameScansTrustedRoots(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "alpha"), "alpha", "same")
	report, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skills) != 1 || report.Skills[0].ContentHash == "" {
		t.Fatalf("unexpected scan report: %#v", report.Skills)
	}
	contentHash := report.Skills[0].ContentHash
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cacheDir := filepath.Join(stateDir, "llm-cache")
	writeFile(t, llm.SummaryCachePath(cacheDir, contentHash), `{"summary":"cached"}`)

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"reset", "llm-summary", "alpha", "--yes", "--root", root, "--trust-root", root, "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(llm.SummaryCachePath(cacheDir, contentHash)); !os.IsNotExist(err) {
		t.Fatalf("expected skill summary cache to be removed, err=%v", err)
	}
	if got := out.String(); !strings.Contains(got, contentHash) || !strings.Contains(got, "Removed 1 cached LLM summary") {
		t.Fatalf("unexpected reset llm-summary output:\n%s", got)
	}
}

func TestAttachLLMSummariesSkipsDisabledAnalyzerOutput(t *testing.T) {
	skills := []inventory.Skill{{Name: "alpha", ContentHash: "hash-a"}}
	enriched := attachLLMSummaries(skills, map[string]llm.GeneratedSummary{
		"hash-a": {Name: "alpha", Summary: "deterministic description", Provider: "disabled", Model: "disabled", ContentHash: "hash-a"},
	})
	if enriched[0].LLMSummary != "" || enriched[0].LLMProvider != "" || enriched[0].LLMModel != "" {
		t.Fatalf("disabled summaries should not be attached as LLM output: %#v", enriched[0])
	}
}

func TestAuditOutputWithFixtureRoot(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "demo", "same")
	writeSkill(t, filepath.Join(root, "b"), "demo", "same")
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Skills scanned: 2", "duplicate: 1", "Open `unlearn`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit output missing %q:\n%s", want, got)
		}
	}
}

func TestAuditFixQuarantinesWritableExactDuplicate(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "demo", "same")
	writeSkill(t, filepath.Join(root, "b"), "demo", "same")
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--fix", "--yes", "--root", root, "--write-root", root, "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "quarantine exact duplicate") || !strings.Contains(got, "quarantined demo") {
		t.Fatalf("unexpected output:\n%s", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("root entries=%d", len(entries))
	}
	matches, err := filepath.Glob(filepath.Join(stateDir, "quarantine", "*", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantine matches=%v", matches)
	}
}

func TestAuditFixWithoutWriteRootIsDryRunOnly(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "demo", "same")
	writeSkill(t, filepath.Join(root, "b"), "demo", "same")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--fix", "--yes", "--root", root, "--trust-root", root, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "requires --write-root") || strings.Contains(got, "quarantined demo") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestAuditHistoryJSONLAddsUnseenFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	writeSkill(t, filepath.Join(root, "b"), "beta", "same")
	historyPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(historyPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--history-jsonl", historyPath, "--state-dir", t.TempDir(), "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "unseen: 1") {
		t.Fatalf("unexpected output:\n%s", got)
	}
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "history_scan = true") || !strings.Contains(string(cfg), historyPath) {
		t.Fatalf("history opt-in/paths not persisted:\n%s", cfg)
	}
}

func TestScanPrintsHistoryProgressAndIndexesEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	historyPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(historyPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"scan", "--root", root, "--trust-root", root, "--history-jsonl", historyPath, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"History scanned:", "1 lines", "1 skills with derived evidence", "Indexed 1 skills"} {
		if !strings.Contains(got, want) {
			t.Fatalf("scan output missing %q:\n%s", want, got)
		}
	}
}

func TestAuditHistorySQLiteAddsUnseenFindings(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	writeSkill(t, filepath.Join(root, "b"), "beta", "same")
	historyPath := filepath.Join(t.TempDir(), "session.db")
	db, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (message TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (message) VALUES ('use the alpha skill')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--history-sqlite", historyPath, "--state-dir", t.TempDir(), "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "unseen: 1") {
		t.Fatalf("unexpected output:\n%s", got)
	}
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "history_scan = true") || !strings.Contains(string(cfg), historyPath) {
		t.Fatalf("SQLite history opt-in/path not persisted:\n%s", cfg)
	}
}

func TestConfiguredRootSQLiteHistoryIsDiscoveredWhenOptedIn(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	writeSkill(t, filepath.Join(root, "b"), "beta", "same")
	historyPath := filepath.Join(root, "history", "session.db")
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (message TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (message) VALUES ('use the alpha skill')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.SetupComplete = true
	cfg.HistoryScan = true
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--state-dir", t.TempDir(), "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "unseen: 1") {
		t.Fatalf("expected discovered configured-root SQLite evidence:\n%s", got)
	}
}

func TestJSONLAndSQLiteHistoryUseSharedHistoryCache(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	jsonlPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlitePath := filepath.Join(t.TempDir(), "session.db")
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (message TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (message) VALUES ('using alpha')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	opts := &cliOptions{
		roots:         []string{root},
		trustRoots:    []string{root},
		historyJSONL:  []string{jsonlPath},
		historySQLite: []string{sqlitePath},
		stateDir:      stateDir,
		configPath:    filepath.Join(t.TempDir(), "config.toml"),
	}
	if _, _, _, err := loadInventoryWithOptions(opts, inventoryLoadOptions{}); err != nil {
		t.Fatal(err)
	}
	cacheDB, err := sql.Open("sqlite", filepath.Join(stateDir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cacheDB.Close()
	var sourceCount int
	if err := cacheDB.QueryRow(`SELECT COUNT(*) FROM history_sources`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 2 {
		t.Fatalf("history source cache count=%d", sourceCount)
	}
	var evidenceCount int
	if err := cacheDB.QueryRow(`SELECT COUNT(*) FROM history_evidence`).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if evidenceCount != 2 {
		t.Fatalf("history evidence cache count=%d", evidenceCount)
	}
}

func TestSQLiteHistoryProgressIsForwardedDuringLoad(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	historyPath := filepath.Join(t.TempDir(), "session.db")
	db, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (message TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (message) VALUES ('use the alpha skill')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	opts := &cliOptions{
		roots:         []string{root},
		trustRoots:    []string{root},
		historySQLite: []string{historyPath},
		stateDir:      t.TempDir(),
		configPath:    filepath.Join(t.TempDir(), "config.toml"),
	}
	var progress []history.ScanProgress
	_, _, _, err = loadInventoryWithOptions(opts, inventoryLoadOptions{HistoryProgress: func(item history.ScanProgress) {
		progress = append(progress, item)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) == 0 || !progress[len(progress)-1].Done || progress[len(progress)-1].Path != historyPath || progress[len(progress)-1].Matches != 1 {
		t.Fatalf("SQLite history progress was not forwarded: %#v", progress)
	}
}

func TestAuditWithLLMPersistsOptIn(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--with-llm", "--state-dir", t.TempDir(), "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "llm_assisted = true") {
		t.Fatalf("LLM opt-in not persisted:\n%s", cfg)
	}
	if !strings.Contains(out.String(), "GEMINI_API_KEY/GOOGLE_API_KEY is not set") {
		t.Fatalf("missing deterministic fallback warning:\n%s", out.String())
	}
}

func TestAuditWithLLMPrintsProgressToErr(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	writeSkill(t, filepath.Join(root, "b"), "beta", "same")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "Find semantic overlap") {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"overlaps\":[]}"}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Short summary."}]}}]}`))
	}))
	defer server.Close()
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("UNLEARN_GEMINI_BASE_URL", server.URL)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--with-llm", "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	progress := errOut.String()
	for _, want := range []string{"Scan skill roots", "Run deterministic checks", "Generate Gemini summaries", "Find semantic overlaps"} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress output missing %q:\n%s", want, progress)
		}
	}
	if strings.Contains(out.String(), "Generate Gemini summaries") {
		t.Fatalf("progress leaked to stdout:\n%s", out.String())
	}
}

func TestAuditInactiveAgentRootFinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeRoot := filepath.Join(home, ".claude", "skills")
	writeSkill(t, filepath.Join(claudeRoot, "legacy"), "legacy", "same")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--trust-root", claudeRoot, "--active-agent", "pi", "--inactive-agent", "claude-code", "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "inactive-harness-root: 1") {
		t.Fatalf("expected inactive harness finding:\n%s", got)
	}
}

func TestAuditSkipsUntrustedRoot(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "demo", "same")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "Skills scanned: 0") || !strings.Contains(got, "Skipped untrusted roots") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: fixture skill\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
