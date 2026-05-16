package config

import (
	"path/filepath"
	"testing"
)

func TestConfigDecisionHelpers(t *testing.T) {
	cfg := Default()
	cfg.KeepSkill("alpha")
	cfg.KeepSkill("alpha")
	cfg.MarkDropCandidate("beta")
	cfg.IgnoreFinding("overlap:a:b", "known overlap")
	if len(cfg.Keep.Skills) != 1 || cfg.Keep.Skills[0] != "alpha" {
		t.Fatalf("keep=%v", cfg.Keep.Skills)
	}
	if len(cfg.DropCandidates.Skills) != 1 || cfg.DropCandidates.Skills[0] != "beta" {
		t.Fatalf("drop=%v", cfg.DropCandidates.Skills)
	}
	if cfg.IgnoreFindings["overlap:a:b"] != "known overlap" {
		t.Fatalf("ignore=%v", cfg.IgnoreFindings)
	}
}

func TestConfigTrustAndWriteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.SetupComplete = true
	cfg.HistoryScan = true
	cfg.HistoryJSONL = []string{"/tmp/session.jsonl"}
	cfg.TrustRoot("/tmp/skills")
	cfg.AllowWrite("/tmp/skills")
	cfg.Keep.Skills = []string{"keep-me"}
	cfg.IgnoreFindings = map[string]string{"overlap:a:b": "known"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SetupComplete || !loaded.HistoryScan || len(loaded.HistoryJSONL) != 1 {
		t.Fatalf("setup/history did not round-trip: %#v", loaded)
	}
	if !loaded.IsTrusted("/tmp/skills") || !loaded.CanWrite("/tmp/skills") {
		t.Fatalf("trust/write did not round-trip: %#v", loaded)
	}
	if len(loaded.Keep.Skills) != 1 || loaded.Keep.Skills[0] != "keep-me" {
		t.Fatalf("keep decisions did not round-trip: %#v", loaded.Keep)
	}
	if loaded.IgnoreFindings["overlap:a:b"] != "known" {
		t.Fatalf("ignore decisions did not round-trip: %#v", loaded.IgnoreFindings)
	}
}
