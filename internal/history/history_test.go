package history

import (
	"bufio"
	"context"
	"database/sql"
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

func TestSQLiteAdapterScansTextColumnsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE sessions (id INTEGER PRIMARY KEY, message TEXT, metadata JSON, count INTEGER)`,
		`INSERT INTO sessions (message, metadata, count) VALUES ('read /tmp/skills/alpha/SKILL.md', '{}', 1)`,
		`INSERT INTO sessions (message, metadata, count) VALUES ('ordinary row', '{"note":"using beta"}', 2)`,
		`CREATE TABLE numbers (value INTEGER)`,
		`INSERT INTO numbers (value) VALUES (12345)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	evidence, err := SQLiteAdapter{}.Scan(path, []string{"alpha", "beta", "12345"})
	if err != nil {
		t.Fatal(err)
	}
	grades := map[string]EvidenceGrade{}
	seen := map[string]bool{}
	for _, item := range evidence {
		grades[item.SkillName] = item.Grade
		seen[item.SkillName] = !item.SeenAt.IsZero()
	}
	if grades["alpha"] != EvidenceStrong {
		t.Fatalf("alpha grade=%s", grades["alpha"])
	}
	if grades["beta"] != EvidenceStrong {
		t.Fatalf("beta grade=%s", grades["beta"])
	}
	if _, ok := grades["12345"]; ok {
		t.Fatalf("numeric-only table produced evidence: %v", grades)
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Fatalf("expected SQLite evidence to carry last-seen fallback: %v", seen)
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
