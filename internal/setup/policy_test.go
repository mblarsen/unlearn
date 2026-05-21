package setup

import (
	"reflect"
	"testing"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestPolicyDerivesChoicesFromConfigAndStatuses(t *testing.T) {
	cfg := config.Default()
	cfg.TrustRoot("/skills")
	cfg.LLMAssisted = true
	cfg.HistoryScan = true
	cfg.ActiveAgents = []string{"amp"}
	cfg.InactiveAgents = []string{"pi"}

	policy := NewPolicy(
		[]RootChoice{{Path: "/skills", Exists: true}, {Path: "/other", Exists: true}},
		[]string{"/history/a.jsonl"},
		[]string{"/history/a.db"},
		cfg,
		[]inventory.AgentStatus{
			{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true},
			{AgentDefinition: inventory.AgentDefinition{ID: "amp", DisplayName: "Amp", ShowInSetup: true}},
			{AgentDefinition: inventory.AgentDefinition{ID: "hidden", DisplayName: "Hidden"}, Installed: true},
		},
	)

	if !policy.Roots[0].Trusted || policy.Roots[1].Trusted {
		t.Fatalf("trusted roots not derived from config: %#v", policy.Roots)
	}
	if !policy.LLMEnabled || !policy.HistoryScan {
		t.Fatalf("options not derived from config: %#v", policy)
	}
	if len(policy.Agents) != 2 {
		t.Fatalf("hidden agents should not be shown: %#v", policy.Agents)
	}
	if !policy.Agents[0].Inactive || policy.Agents[0].Active || !policy.Agents[0].Detected {
		t.Fatalf("pi state not derived from config/status: %#v", policy.Agents[0])
	}
	if !policy.Agents[1].Active || policy.Agents[1].Inactive {
		t.Fatalf("amp state not derived from config/status: %#v", policy.Agents[1])
	}
}

func TestPolicyDefaultsActiveAgentsFromInstalledAndDefaultHarnesses(t *testing.T) {
	active, inactive := SelectAgentIDs(nil, nil, config.Default(), []inventory.AgentStatus{
		{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}},
		{AgentDefinition: inventory.AgentDefinition{ID: "codex", DisplayName: "Codex", ShowInSetup: true}},
		{AgentDefinition: inventory.AgentDefinition{ID: "amp", DisplayName: "Amp", ShowInSetup: true}, Installed: true},
		{AgentDefinition: inventory.AgentDefinition{ID: "hidden", DisplayName: "Hidden"}, Installed: true},
	})

	want := []string{"amp", "codex", "pi"}
	if !reflect.DeepEqual(active, want) || len(inactive) != 0 {
		t.Fatalf("default active/inactive=%v/%v, want %v/[]", active, inactive, want)
	}
}

func TestPolicySelectAgentIDsPrefersOverridesThenConfig(t *testing.T) {
	cfg := config.Default()
	cfg.ActiveAgents = []string{"pi"}
	cfg.InactiveAgents = []string{"codex"}
	statuses := []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "amp", DisplayName: "Amp", ShowInSetup: true}, Installed: true}}

	active, inactive := SelectAgentIDs([]string{"opencode"}, []string{"cline"}, cfg, statuses)
	if !reflect.DeepEqual(active, []string{"opencode"}) || !reflect.DeepEqual(inactive, []string{"cline"}) {
		t.Fatalf("overrides should win: active=%v inactive=%v", active, inactive)
	}

	active, inactive = SelectAgentIDs(nil, nil, cfg, statuses)
	if !reflect.DeepEqual(active, []string{"pi"}) || !reflect.DeepEqual(inactive, []string{"codex"}) {
		t.Fatalf("config should win over defaults: active=%v inactive=%v", active, inactive)
	}
}

func TestPolicyApplyPreservesOptInHistoryBehavior(t *testing.T) {
	policy := Policy{
		Roots:         []RootChoice{{Path: "/skills", Trusted: true}},
		Agents:        []AgentChoice{{ID: "pi", Active: true}, {ID: "codex", Inactive: true}},
		HistoryJSONL:  []string{"/history/a.jsonl"},
		HistorySQLite: []string{"/history/a.db"},
		LLMEnabled:    true,
		HistoryScan:   true,
	}

	cfg := policy.Apply(config.Default())
	if !cfg.SetupComplete || !cfg.IsTrusted("/skills") || !cfg.LLMAssisted || !cfg.HistoryScan {
		t.Fatalf("policy not applied: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.ActiveAgents, []string{"pi"}) || !reflect.DeepEqual(cfg.InactiveAgents, []string{"codex"}) {
		t.Fatalf("agent selection not applied: active=%v inactive=%v", cfg.ActiveAgents, cfg.InactiveAgents)
	}
	if !reflect.DeepEqual(cfg.HistoryJSONL, []string{"/history/a.jsonl"}) || !reflect.DeepEqual(cfg.HistorySQLite, []string{"/history/a.db"}) {
		t.Fatalf("history sources not applied: jsonl=%v sqlite=%v", cfg.HistoryJSONL, cfg.HistorySQLite)
	}

	policy.HistoryScan = false
	cfg = policy.Apply(cfg)
	if cfg.HistoryScan || cfg.HistoryJSONL != nil || cfg.HistorySQLite != nil {
		t.Fatalf("history sources should clear without opt-in: %#v", cfg)
	}
}
