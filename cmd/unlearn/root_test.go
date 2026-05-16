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
