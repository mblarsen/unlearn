package config

import (
	"path/filepath"
	"testing"

	"github.com/mblarsen/unlearn/internal/collections"
	"github.com/mblarsen/unlearn/internal/review"
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
	cfg.HistorySQLite = []string{"/tmp/session.db"}
	cfg.ActiveAgents = []string{"pi", "codex"}
	cfg.InactiveAgents = []string{"claude-code"}
	cfg.TrustRoot("/tmp/skills")
	cfg.AllowWrite("/tmp/skills")
	cfg.Keep.Skills = []string{"keep-me"}
	cfg.IgnoreFindings = map[string]string{"overlap:a:b": "known"}
	cfg.GuidedReview = review.State{
		Scope:     []review.ScopeItem{{ID: "item-1", FindingID: "unseen:alpha", SkillName: "alpha", InstallPath: "/tmp/skills/alpha"}},
		Decisions: []review.Decision{{ItemID: "item-1", Action: review.ActionRevisit}},
	}
	cfg.Collections = []collections.Collection{{Name: "Frontend", Members: []collections.Member{{SkillName: "alpha", InstallPath: "/tmp/skills/alpha"}}}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SetupComplete || !loaded.HistoryScan || len(loaded.HistoryJSONL) != 1 || len(loaded.HistorySQLite) != 1 {
		t.Fatalf("setup/history did not round-trip: %#v", loaded)
	}
	if len(loaded.ActiveAgents) != 2 || len(loaded.InactiveAgents) != 1 || !loaded.HasAgentSelection() {
		t.Fatalf("agent selections did not round-trip: %#v", loaded)
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
	if len(loaded.GuidedReview.Scope) != 1 || len(loaded.GuidedReview.Decisions) != 1 || loaded.GuidedReview.Decisions[0].Action != review.ActionRevisit {
		t.Fatalf("guided review did not round-trip: %#v", loaded.GuidedReview)
	}
	if len(loaded.Collections) != 1 || len(loaded.Collections[0].Members) != 1 || loaded.Collections[0].Members[0].InstallPath != "/tmp/skills/alpha" {
		t.Fatalf("collections did not round-trip: %#v", loaded.Collections)
	}
}
