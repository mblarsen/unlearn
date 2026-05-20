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
	content := `{"message":"read /tmp/skills/alpha/SKILL.md"}` + "\n" +
		`{"message":"beta was mentioned only"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := JSONLAdapter{}.Scan(path, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	grades := map[string]EvidenceGrade{}
	for _, item := range evidence {
		grades[item.SkillName] = item.Grade
	}
	if grades["alpha"] != EvidenceStrong {
		t.Fatalf("alpha grade=%s", grades["alpha"])
	}
	if grades["beta"] != EvidenceWeak {
		t.Fatalf("beta grade=%s", grades["beta"])
	}
}
