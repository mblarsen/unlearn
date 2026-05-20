package history

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLAdapterReportsProgressAndSupportsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var content string
	for i := 0; i < 600; i++ {
		content += fmt.Sprintf(`{"message":"line %d alpha"}`+"\n", i)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var progress []ScanProgress
	_, err := JSONLAdapter{}.ScanWithOptions(path, []string{"alpha"}, ScanOptions{Progress: func(item ScanProgress) {
		progress = append(progress, item)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 || !progress[len(progress)-1].Done || progress[len(progress)-1].Lines != 600 {
		t.Fatalf("unexpected progress: %#v", progress)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = JSONLAdapter{}.ScanWithOptions(path, []string{"alpha"}, ScanOptions{Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestJSONLAdapterHandlesLongJSONLLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"message":"` + strings.Repeat("x", bufio.MaxScanTokenSize+1) + ` alpha skill.md"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := JSONLAdapter{}.Scan(path, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Grade != EvidenceStrong {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestJSONLAdapterScansDerivedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"timestamp":"2026-01-02T03:04:05Z","message":"read /tmp/skills/alpha/SKILL.md"}` + "\n" +
		`{"timestamp":"2026-01-03T03:04:05Z","message":"beta was mentioned only"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := JSONLAdapter{}.Scan(path, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	grades := map[string]EvidenceGrade{}
	seen := map[string]string{}
	for _, item := range evidence {
		grades[item.SkillName] = item.Grade
		if !item.SeenAt.IsZero() {
			seen[item.SkillName] = item.SeenAt.Format(time.RFC3339)
		}
	}
	if grades["alpha"] != EvidenceStrong {
		t.Fatalf("alpha grade=%s", grades["alpha"])
	}
	if grades["beta"] != EvidenceWeak {
		t.Fatalf("beta grade=%s", grades["beta"])
	}
	if seen["alpha"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("alpha seen=%s", seen["alpha"])
	}
	if seen["beta"] != "" {
		t.Fatalf("weak beta mention should not get last-used timestamp: %s", seen["beta"])
	}
}
