package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestMissingSourceStopsTrackingUntilRescan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	cfg := config.Default()
	cfg.HistoryScan = true
	cfg.HistoryJSONL = []string{path}
	opts := Options{Config: cfg, Paths: testStatePaths(t), Skills: []inventory.Skill{{Name: "alpha"}}, HistoryCacheTTL: time.Hour}
	first, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.StoppedSources) != 1 {
		t.Fatalf("first notification: %+v", first)
	}
	second, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.StoppedSources) != 0 || len(second.MissingSources) != 1 {
		t.Fatalf("repeated warning or lost coverage: %+v", second)
	}
	if err := os.WriteFile(path, []byte("{\"message\":\"read skills/alpha/SKILL.md\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.MissingSources) != 1 || third.Evidence["alpha"] != "" {
		t.Fatalf("inactive source scanned: %+v", third)
	}
	opts.RescanSources = true
	recovered, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.MissingSources) != 0 || recovered.Evidence["alpha"] != "strong" {
		t.Fatalf("rescan did not restore source: %+v", recovered)
	}
}
