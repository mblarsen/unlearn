package unlearn

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestAuditWithLLMPersistsOptIn(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), "alpha", "same")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cmd := newRootCmd(&bytes.Buffer{})
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
