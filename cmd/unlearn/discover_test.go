package unlearn

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverCommandPrintsRankedReasonsAndExactInstalls(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "review"), "pull-request-review", "Review pull requests for security issues")
	writeSkill(t, filepath.Join(root, "notes"), "notes", "Create local notes")

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"discover", "review pull requests", "--root", root, "--trust-root", root, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Task: review pull requests", "1 matching installed skill", "pull-request-review", "name matches", filepath.Join(root, "review"), "Agent access: unknown", "Invocation: not observed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("discover output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "notes") || strings.Contains(strings.ToLower(got), "confidence") {
		t.Fatalf("discover output includes an unmatched skill or invented confidence:\n%s", got)
	}
}

func TestDiscoverCommandExplainsNoObservedMatch(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "notes"), "notes", "Create local notes")

	var out bytes.Buffer
	cmd := newRootCmd(&out)
	cmd.SetArgs([]string{"discover", "deploy kubernetes", "--root", root, "--trust-root", root, "--state-dir", t.TempDir(), "--config", filepath.Join(t.TempDir(), "config.toml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "No observed matching installed skill for this task.") {
		t.Fatalf("unexpected no-match output:\n%s", got)
	}
}
