package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/history"
)

func TestHistoryCacheFreshnessAndRoundTrip(t *testing.T) {
	db, err := OpenIndex(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := "/sessions/a.jsonl"
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seenAt := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	if err := SaveHistoryEvidence(db, source, mtime, []history.Evidence{{SkillName: "alpha", Grade: history.EvidenceStrong, Source: source, SeenAt: seenAt}}); err != nil {
		t.Fatal(err)
	}

	status, err := HistoryCacheStatusForSource(db, source, mtime, 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Fresh {
		t.Fatalf("expected fresh cache: %#v", status)
	}

	evidence, err := LoadHistoryEvidence(db, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].SkillName != "alpha" || evidence[0].Grade != history.EvidenceStrong || !evidence[0].SeenAt.Equal(seenAt) {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}

	changedStatus, err := HistoryCacheStatusForSource(db, source, mtime.Add(time.Second), 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if changedStatus.Fresh {
		t.Fatalf("changed source mtime should invalidate cache")
	}
}

func TestHistoryCacheFreshWhenNoEvidenceWasFound(t *testing.T) {
	db, err := OpenIndex(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := "/sessions/empty.jsonl"
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := SaveHistoryEvidence(db, source, mtime, nil); err != nil {
		t.Fatal(err)
	}
	status, err := HistoryCacheStatusForSource(db, source, mtime, 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Fresh {
		t.Fatalf("empty scan results should still be cached")
	}
}
