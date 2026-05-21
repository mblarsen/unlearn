package setup

import (
	"os"
	"sort"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// Policy is the first-launch setup Module: it owns the rules that connect
// trusted skill roots, global skill inventory choices, and active/inactive
// agent harness selection. The TUI model is intentionally an Adapter over this
// structure so rendering and keyboard handling stay shallow.
type Policy struct {
	Roots         []RootChoice
	Agents        []AgentChoice
	HistoryJSONL  []string
	HistorySQLite []string
	LLMEnabled    bool
	HistoryScan   bool
}

// NewPolicy derives setup choices from persisted config, candidate trusted skill
// roots, discovered history sources, and the global skill inventory status.
func NewPolicy(roots []RootChoice, historyJSONL []string, historySQLite []string, cfg config.Config, statuses []inventory.AgentStatus) Policy {
	choices := make([]RootChoice, len(roots))
	for i, root := range roots {
		root.Trusted = cfg.IsTrusted(root.Path)
		choices[i] = root
	}
	return Policy{
		Roots:         choices,
		Agents:        agentChoices(cfg, statuses),
		HistoryJSONL:  append([]string(nil), historyJSONL...),
		HistorySQLite: append([]string(nil), historySQLite...),
		LLMEnabled:    cfg.LLMAssisted,
		HistoryScan:   cfg.HistoryScan,
	}
}

// RootChoices probes candidate roots once at the setup boundary. The policy
// keeps the trusted decision separate from this Adapter-level existence signal.
func RootChoices(roots []string) []RootChoice {
	choices := make([]RootChoice, 0, len(roots))
	for _, root := range roots {
		_, err := os.Stat(root)
		choices = append(choices, RootChoice{Path: root, Exists: err == nil})
	}
	return choices
}

// CandidateRoots centralizes the first-launch setup decision for which global
// skill inventory roots are shown. Explicit roots have highest leverage;
// otherwise the active/inactive harness policy supplies roots, with the catalog
// fallback preserving historical behavior.
func CandidateRoots(explicitRoots []string, cfg config.Config, statuses []inventory.AgentStatus) []string {
	if len(explicitRoots) > 0 {
		return append([]string(nil), explicitRoots...)
	}
	active, inactive := SelectAgentIDs(nil, nil, cfg, statuses)
	roots := inventory.RootsForAgents(append(active, inactive...))
	if len(roots) == 0 {
		roots = inventory.KnownGlobalRoots()
	}
	return roots
}

// SelectAgentIDs is the setup policy seam for active/inactive agent harnesses.
// CLI overrides win, then persisted config, then first-launch defaults from the
// global skill inventory status.
func SelectAgentIDs(activeOverride []string, inactiveOverride []string, cfg config.Config, statuses []inventory.AgentStatus) ([]string, []string) {
	if len(activeOverride) > 0 || len(inactiveOverride) > 0 {
		return append([]string(nil), activeOverride...), append([]string(nil), inactiveOverride...)
	}
	if cfg.HasAgentSelection() {
		return append([]string(nil), cfg.ActiveAgents...), append([]string(nil), cfg.InactiveAgents...)
	}
	return defaultAgentIDs(statuses), nil
}

// Apply persists setup decisions back to config while preserving the TOML shape.
func (p Policy) Apply(cfg config.Config) config.Config {
	for _, root := range p.Roots {
		if root.Trusted {
			cfg.TrustRoot(root.Path)
		}
	}
	cfg.ActiveAgents = nil
	cfg.InactiveAgents = nil
	for _, agent := range p.Agents {
		if agent.Active {
			cfg.ActiveAgents = append(cfg.ActiveAgents, agent.ID)
		} else if agent.Inactive {
			cfg.InactiveAgents = append(cfg.InactiveAgents, agent.ID)
		}
	}
	cfg.LLMAssisted = p.LLMEnabled
	cfg.HistoryScan = p.HistoryScan
	if p.HistoryScan {
		cfg.HistoryJSONL = append([]string(nil), p.HistoryJSONL...)
		cfg.HistorySQLite = append([]string(nil), p.HistorySQLite...)
	} else {
		cfg.HistoryJSONL = nil
		cfg.HistorySQLite = nil
	}
	cfg.SetupComplete = true
	return cfg
}

func agentChoices(cfg config.Config, statuses []inventory.AgentStatus) []AgentChoice {
	active, inactive := SelectAgentIDs(nil, nil, cfg, statuses)
	activeSet := idSet(active)
	inactiveSet := idSet(inactive)
	choices := []AgentChoice{}
	for _, status := range statuses {
		if !status.ShowInSetup {
			continue
		}
		choice := AgentChoice{ID: status.ID, Name: status.DisplayName, Detected: status.Installed}
		choice.Active = activeSet[status.ID]
		choice.Inactive = inactiveSet[status.ID]
		choices = append(choices, choice)
	}
	return choices
}

func defaultAgentIDs(statuses []inventory.AgentStatus) []string {
	active := []string{}
	for _, status := range statuses {
		if !status.ShowInSetup {
			continue
		}
		if status.Installed || defaultActiveAgent(status.ID) {
			active = append(active, status.ID)
		}
	}
	sort.Strings(active)
	return active
}

func defaultActiveAgent(id string) bool {
	switch id {
	case "pi", "codex", "opencode", "cline":
		return true
	default:
		return false
	}
}

func idSet(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set
}
