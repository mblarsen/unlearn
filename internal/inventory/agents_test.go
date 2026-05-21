package inventory

import (
	"path/filepath"
	"testing"
)

func TestAgentCatalogIncludesVercelSkillRoots(t *testing.T) {
	agents := map[string]AgentDefinition{}
	for _, agent := range AgentCatalog() {
		agents[agent.ID] = agent
	}
	for _, id := range []string{"pi", "codex", "opencode", "claude-code", "cursor", "goose"} {
		if agents[id].ID == "" {
			t.Fatalf("missing agent %s", id)
		}
	}
	if got := filepath.ToSlash(agents["pi"].GlobalSkillsDir); !hasSuffix(got, "/.pi/agent/skills") {
		t.Fatalf("pi root=%s", got)
	}
	if got := filepath.ToSlash(agents["opencode"].GlobalSkillsDir); !hasSuffix(got, "/opencode/skills") {
		t.Fatalf("opencode root=%s", got)
	}
	if agents["claude-code"].UsesAgentsSkillsRoot {
		t.Fatalf("claude-code should not use ~/.agents/skills")
	}
}

func TestRootsForAgentsIncludesSharedAgentsRootOnlyForMarkedHarnesses(t *testing.T) {
	piRoots := RootsForAgents([]string{"pi"})
	if !containsRootWithSuffix(piRoots, "/.pi/agent/skills") {
		t.Fatalf("expected pi roots to include ~/.pi/agent/skills, got %v", piRoots)
	}
	if !containsRootWithSuffix(piRoots, "/.agents/skills") {
		t.Fatalf("expected marked pi roots to include ~/.agents/skills, got %v", piRoots)
	}

	claudeRoots := RootsForAgents([]string{"claude-code"})
	if containsRootWithSuffix(claudeRoots, "/.agents/skills") {
		t.Fatalf("expected claude roots not to include ~/.agents/skills, got %v", claudeRoots)
	}
}

func TestRootsForAgentsDedupesSharedRoots(t *testing.T) {
	roots := RootsForAgents([]string{"cline", "warp"})
	if len(roots) != 1 {
		t.Fatalf("expected shared ~/.agents/skills root once, got %v", roots)
	}
}

func TestRootOwnershipForAgentsSeparatesActiveAndInactive(t *testing.T) {
	owners := RootOwnershipForAgents([]string{"pi"}, []string{"claude-code"})
	var piRoot, claudeRoot string
	for _, agent := range AgentCatalog() {
		switch agent.ID {
		case "pi":
			piRoot = filepath.Clean(agent.GlobalSkillsDir)
		case "claude-code":
			claudeRoot = filepath.Clean(agent.GlobalSkillsDir)
		}
	}
	if len(owners[piRoot].ActiveAgents) != 1 || owners[piRoot].ActiveAgents[0] != "pi" {
		t.Fatalf("pi ownership=%#v", owners[piRoot])
	}
	agentsRoot := filepath.Clean(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(piRoot))), ".agents", "skills"))
	if len(owners[agentsRoot].ActiveAgents) != 1 || owners[agentsRoot].ActiveAgents[0] != "pi" {
		t.Fatalf("pi ~/.agents ownership=%#v", owners[agentsRoot])
	}
	if len(owners[claudeRoot].InactiveAgents) != 1 || owners[claudeRoot].InactiveAgents[0] != "claude-code" {
		t.Fatalf("claude ownership=%#v", owners[claudeRoot])
	}
	if len(owners[agentsRoot].InactiveAgents) != 0 {
		t.Fatalf("claude should not own ~/.agents, got %#v", owners[agentsRoot])
	}
}

func containsRootWithSuffix(roots []string, suffix string) bool {
	for _, root := range roots {
		if hasSuffix(filepath.ToSlash(root), suffix) {
			return true
		}
	}
	return false
}

func hasSuffix(value, suffix string) bool {
	if len(value) < len(suffix) {
		return false
	}
	return value[len(value)-len(suffix):] == suffix
}
