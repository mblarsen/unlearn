package analysis

import (
	"testing"

	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestAnalyzeDetectsDuplicateConflictAndBroken(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "same", ID: "a", ContentHash: "h1", ActiveAgents: []string{"pi"}},
		{Name: "same", ID: "b", ContentHash: "h1", ActiveAgents: []string{"pi"}},
		{Name: "conflict", ID: "c", ContentHash: "h1", ActiveAgents: []string{"pi"}},
		{Name: "conflict", ID: "d", ContentHash: "h2", ActiveAgents: []string{"pi"}},
		{Name: "broken", ID: "e", BrokenRefs: []string{"references/missing.md"}},
	}
	findings := Analyze(skills, Options{})
	assertHasType(t, findings, FindingDuplicate)
	assertHasType(t, findings, FindingConflict)
	assertHasType(t, findings, FindingBroken)
}

func TestAnalyzeDetectsOverlapHighTokenBroadActivationAndUnseen(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "react-ui", ID: "a", Description: "React component frontend design accessible", Body: "dashboard layout responsive", ContentHash: "a", UpperTokens: 3000, LowerTokens: 100, ActivationRisk: "high"},
		{Name: "vue-ui", ID: "b", Description: "Vue component frontend design accessible", Body: "dashboard layout responsive", ContentHash: "b", UpperTokens: 10, LowerTokens: 5, ActivationRisk: "low"},
	}
	findings := Analyze(skills, Options{UsageEvidence: UsageEvidence{"react-ui": "strong"}, HighTokenLimit: 2000})
	assertHasType(t, findings, FindingOverlap)
	assertHasType(t, findings, FindingHighTokenCost)
	assertHasType(t, findings, FindingBroadActivation)
	assertHasType(t, findings, FindingUnseen)
}

func TestAnalyzeUsageEvidenceMatchesSkillNamesCaseInsensitively(t *testing.T) {
	skills := []inventory.Skill{{Name: "MySkill", ID: "a", ContentHash: "a"}}
	findings := Analyze(skills, Options{UsageEvidence: UsageEvidence{"myskill": "strong"}})
	if countType(findings, FindingUnseen) != 0 {
		t.Fatalf("mixed-case skill was falsely marked unseen: %#v", findings)
	}
}

func TestAnalyzeConsolidatesSameNameSingleSkillFindings(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "macos-calendar", ID: "a", ContentHash: "a", UpperTokens: 5000, LowerTokens: 2000, ActivationRisk: "high", Root: "/one"},
		{Name: "macos-calendar", ID: "b", ContentHash: "b", UpperTokens: 7800, LowerTokens: 4100, ActivationRisk: "high", Root: "/two"},
	}
	findings := Analyze(skills, Options{HighTokenLimit: 2000})
	if countType(findings, FindingHighTokenCost) != 1 {
		t.Fatalf("expected one consolidated token finding, got %#v", findings)
	}
	if countType(findings, FindingBroadActivation) != 1 {
		t.Fatalf("expected one consolidated activation finding, got %#v", findings)
	}
	for _, finding := range findings {
		if finding.Type == FindingHighTokenCost && len(finding.Skills) != 2 {
			t.Fatalf("expected both installs in finding: %#v", finding)
		}
	}
}

func TestGenericActionWordsDoNotCreateOverlapSpam(t *testing.T) {
	var skills []inventory.Skill
	genericDescription := "plan build create design implement review fix improve optimize enhance refactor check many things across content long product areas"
	names := []string{"macos-calendar", "macos-notes", "macos-reminders", "alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	for _, name := range names {
		skills = append(skills, inventory.Skill{Name: name, Description: genericDescription, Body: "Domain-specific skill with generic trigger words plus enough content to exceed token budgets.", ContentHash: name})
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 0 {
		t.Fatalf("generic action words created overlap spam: %#v", findings)
	}
}

func TestOverlapUsesLogicalSkillNamesForClusters(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "react-a11y", Description: "React frontend accessibility aria keyboard", Body: "dashboard semantic focus", ContentHash: "a", Root: "/one"},
		{Name: "react-a11y", Description: "React frontend accessibility aria keyboard", Body: "dashboard semantic focus", ContentHash: "b", Root: "/two"},
		{Name: "react-forms", Description: "React frontend accessibility aria keyboard", Body: "form semantic focus", ContentHash: "c"},
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 1 {
		t.Fatalf("expected one overlap cluster, got %#v", findings)
	}
	for _, finding := range findings {
		if finding.Type == FindingOverlap && len(finding.Skills) != 2 {
			t.Fatalf("expected logical skills only in overlap, got %#v", finding)
		}
	}
}

func TestDuplicateRequiresSharedActiveHarness(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "shared", ID: "a", ContentHash: "h1", Root: "/pi", ActiveAgents: []string{"pi"}},
		{Name: "shared", ID: "b", ContentHash: "h1", Root: "/codex", ActiveAgents: []string{"codex"}},
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingDuplicate) != 0 {
		t.Fatalf("separate harness roots should not be actionable duplicates: %#v", findings)
	}
	skills[1].ActiveAgents = []string{"pi", "codex"}
	findings = Analyze(skills, Options{})
	if countType(findings, FindingDuplicate) != 1 {
		t.Fatalf("shared active harness should create duplicate: %#v", findings)
	}
}

func TestAnalyzeDetectsInactiveRootFindings(t *testing.T) {
	skills := []inventory.Skill{{Name: "claude-only", ID: "a", ContentHash: "h1", RootKnown: true, InactiveAgents: []string{"claude-code"}}}
	findings := Analyze(skills, Options{})
	assertHasType(t, findings, FindingInactiveRoot)
}

func TestOverlapClustersConnectedComponents(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "react-a11y", Description: "React frontend accessibility aria keyboard", Body: "dashboard semantic focus", ContentHash: "a"},
		{Name: "react-forms", Description: "React frontend accessibility aria keyboard", Body: "form semantic focus", ContentHash: "b"},
		{Name: "react-dashboard", Description: "React frontend accessibility aria keyboard", Body: "dashboard semantic focus", ContentHash: "c"},
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 1 {
		t.Fatalf("expected one overlap cluster, got %#v", findings)
	}
}

func countType(findings []Finding, typ FindingType) int {
	count := 0
	for _, finding := range findings {
		if finding.Type == typ {
			count++
		}
	}
	return count
}

func assertHasType(t *testing.T, findings []Finding, typ FindingType) {
	t.Helper()
	for _, finding := range findings {
		if finding.Type == typ {
			return
		}
	}
	t.Fatalf("missing finding type %s in %#v", typ, findings)
}
