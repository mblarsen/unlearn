package unlearn

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/config"
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

func TestRestoreReconcilesAndPersistsInventorySnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	destRoot := filepath.Join(home, ".pi", "agent", "skills")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo", "override"} {
		stored := filepath.Join(stateDir, "quarantine", "20260909T120000.000000000Z", name)
		if err := os.MkdirAll(stored, 0o755); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: restored fixture\n---\n", name)
		if err := os.WriteFile(filepath.Join(stored, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.SetupComplete = true
	cfg.ActiveAgents = []string{"pi"}
	cfg.TrustRoot(destRoot)
	cfg.AllowWrite(destRoot)
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"restore", "demo", "--to-root", destRoot, "--state-dir", stateDir, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Restored demo to "+filepath.Join(destRoot, "demo")) {
		t.Fatalf("output=%q", out.String())
	}
	db, err := state.OpenIndex(filepath.Join(stateDir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	skills, findings, err := state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(skills) != 1 || skills[0].Name != "demo" || skills[0].EncounteredPath != filepath.Join(destRoot, "demo") || skills[0].ID == "" {
		t.Fatalf("skills=%#v findings=%#v", skills, findings)
	}
	if !skills[0].RootKnown || !reflect.DeepEqual(skills[0].ActiveAgents, []string{"pi"}) {
		t.Fatalf("restored skill lost configured ownership: %#v", skills[0])
	}

	out.Reset()
	cmd = newRootCmd(&out)
	cmd.SetArgs([]string{"restore", "override", "--to-root", destRoot, "--state-dir", stateDir, "--config", configPath, "--active-agent", "codex", "--inactive-agent", "pi"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	skills, _, err = state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		if skill.Name == "override" {
			if len(skill.ActiveAgents) != 0 || !reflect.DeepEqual(skill.InactiveAgents, []string{"pi"}) {
				t.Fatalf("explicit agent flags did not override config: %#v", skill)
			}
			return
		}
	}
	t.Fatalf("override restore missing from snapshot: %#v", skills)
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

func TestAuditConfiguredMissingHistoryWarnsWithoutUnseenFindings(t *testing.T) {
	for _, rescan := range []bool{false, true} {
		t.Run(fmt.Sprintf("rescan=%t", rescan), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root := t.TempDir()
			writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
			missingPath := filepath.Join(t.TempDir(), "deleted-session.jsonl")
			configPath := filepath.Join(t.TempDir(), "config.toml")
			cfg := config.Default()
			cfg.SetupComplete = true
			cfg.HistoryScan = true
			cfg.HistoryJSONL = []string{missingPath}
			cfg.TrustRoot(root)
			if err := cfg.Save(configPath); err != nil {
				t.Fatal(err)
			}
			args := []string{"audit", "--root", root, "--state-dir", t.TempDir(), "--config", configPath}
			if rescan {
				args = append(args, "--rescan-sources")
			}
			var out bytes.Buffer
			cmd := newRootCmd(&out)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("audit failed for stale configured history: %v", err)
			}
			got := out.String()
			if !strings.Contains(got, "Stopped tracking missing history source") || !strings.Contains(got, missingPath) {
				t.Fatalf("missing history diagnostic not printed:\n%s", got)
			}
			if !strings.Contains(got, "unseen: 0") {
				t.Fatalf("incomplete history must not declare skills unseen:\n%s", got)
			}
		})
	}
}

func TestAuditExplicitMissingHistoryStillFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	missingPath := filepath.Join(t.TempDir(), "explicit-missing.jsonl")
	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"audit", "--root", root, "--trust-root", root, "--history-jsonl", missingPath, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit missing history error=%v", err)
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

func TestMissingLLMCredentialsOnlyErrorWhenExplicitlyRequested(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	for _, test := range []struct {
		name      string
		withLLM   bool
		wantError bool
	}{
		{name: "persisted opt-in", wantError: false},
		{name: "explicit flag", withLLM: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
			configPath := filepath.Join(t.TempDir(), "config.toml")
			cfg := config.Default()
			cfg.SetupComplete = true
			cfg.LLMAssisted = true
			cfg.TrustRoot(root)
			if err := cfg.Save(configPath); err != nil {
				t.Fatal(err)
			}
			opts := &cliOptions{roots: []string{root}, configPath: configPath, stateDir: t.TempDir(), withLLM: test.withLLM}
			if _, _, err := runDashboardAudit(opts, inventoryLoadOptions{}, audit.SnapshotCacheBypass); err != nil {
				t.Fatal(err)
			}
			if len(opts.warnings) != 1 || !strings.Contains(opts.warnings[0], "GEMINI_API_KEY/GOOGLE_API_KEY is not set") {
				t.Fatalf("missing informational warning: %v", opts.warnings)
			}
			if got := len(opts.startupErrors) > 0; got != test.wantError {
				t.Fatalf("startup error=%v, want %v; errors=%v", got, test.wantError, opts.startupErrors)
			}
		})
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
		if strings.Contains(string(body), "Review one AI agent skill file") {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"findings\":[]}"}]}}]}`))
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
	for _, want := range []string{"Scan skill roots", "Run deterministic checks", "Generate Gemini summaries", "Find semantic overlaps", "Review skill quality"} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress output missing %q:\n%s", want, progress)
		}
	}
	if strings.Contains(out.String(), "Generate Gemini summaries") {
		t.Fatalf("progress leaked to stdout:\n%s", out.String())
	}
}

func TestPrintAuditShowsSkillQualityAdvisoryCount(t *testing.T) {
	skills := []inventory.Skill{{Name: "alpha", Root: "/one"}}
	findings := []analysis.Finding{{ID: "skill-quality:alpha", Type: analysis.FindingSkillQuality, Severity: 4, Title: "alpha", Skills: skills, Reasons: []string{"LLM-assisted advisory skill-quality: vague_description — too broad Recommendation: be specific (test/fake)"}}}
	var out bytes.Buffer
	printAudit(&out, skills, findings, nil)
	got := out.String()
	for _, want := range []string{"skill-quality: 1", "LLM-assisted advisory recommendations only", "safe fixes ignore them"} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit output missing %q:\n%s", want, got)
		}
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
