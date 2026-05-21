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
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
)

func TestLoadingModelShowsProgress(t *testing.T) {
	updates := make(chan tea.Msg, 1)
	m := newLoadingModel(updates)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	m = updated.(loadingModel)
	updated, _ = m.Update(loadingProgressMsg{progress: history.ScanProgress{Path: "/sessions/a.jsonl", Lines: 500, Matches: 2}})
	m = updated.(loadingModel)

	view := m.View()
	if !strings.Contains(view, "unlearn is loading") || !strings.Contains(view, "Scanning history evidence") || !strings.Contains(view, "500 lines") {
		t.Fatalf("loading view missing progress details:\n%s", view)
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
