package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
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
		{Name: "react-ui", ID: "a", Description: "React frontend interface accessible", Body: "dashboard layout responsive", ContentHash: "a", UpperTokens: 3000, LowerTokens: 100, ActivationRisk: "high"},
		{Name: "vue-ui", ID: "b", Description: "Vue frontend interface accessible", Body: "dashboard layout responsive", ContentHash: "b", UpperTokens: 10, LowerTokens: 5, ActivationRisk: "low"},
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
		{Name: "macos-calendar", ID: "a", ContentHash: "a", UpperTokens: 5000, LowerTokens: 2000, ActivationRisk: "high", ActivationRiskSignals: []string{`universal "must use"`}, Root: "/one"},
		{Name: "macos-calendar", ID: "b", ContentHash: "b", UpperTokens: 7800, LowerTokens: 4100, ActivationRisk: "high", ActivationRiskSignals: []string{`universal "before any"`}, Root: "/two"},
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

func TestBroadActivationReasonsIncludeMatchedSignals(t *testing.T) {
	skills := []inventory.Skill{{
		Name:                  "brainstorming",
		ID:                    "a",
		ContentHash:           "a",
		ActivationRisk:        "high",
		ActivationRiskSignals: []string{`universal "must use"`, `universal "before any"`, `action "plan"`},
	}}
	findings := Analyze(skills, Options{})
	activationFindings := findingsOfType(findings, FindingBroadActivation)
	if len(activationFindings) != 1 {
		t.Fatalf("expected one activation finding, got %#v", findings)
	}
	reason := strings.Join(activationFindings[0].Reasons, "\n")
	for _, want := range []string{`universal "must use"`, `universal "before any"`, `action "plan"`} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason %q missing %q", reason, want)
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

func TestKeywordsFilterCommonProseWords(t *testing.T) {
	terms := keywords("Above text accepts configured support instructions, follows examples, uses optional input, validates configuration, and returns results.")
	for _, word := range []string{"above", "accepts", "configured", "support", "follows", "optional", "uses", "validates", "returns", "results"} {
		if terms[word] {
			t.Fatalf("common prose word %q should not be a keyword: %#v", word, terms)
		}
	}
}

func TestCommonProseWordsDoNotCreateOverlapSpam(t *testing.T) {
	var skills []inventory.Skill
	names := []string{"calendar", "notes", "browser", "fastmail", "wrangler", "review", "launch", "social"}
	for _, name := range names {
		skills = append(skills, inventory.Skill{
			Name:        name,
			Description: "Accepts the request above and uses configured support instructions when available.",
			Body:        "Follow the examples above. Accepts optional input, validates configuration, and returns the result.",
			ContentHash: name,
		})
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 0 {
		t.Fatalf("common prose words created broad overlap findings: %#v", findingsOfType(findings, FindingOverlap))
	}
}

func TestOverlapIgnoresBoilerplateBodyForDescribedSkills(t *testing.T) {
	boilerplate := "absolute abstraction acceptance access accessibility accessible active auth auto bash changes docs each next include data start existing update multiple show verify will"
	skills := []inventory.Skill{
		{Name: "agent-browser", Description: "Control a browser with Playwright automation", Body: boilerplate, ContentHash: "a"},
		{Name: "app-store-review", Description: "Evaluate Apple App Store compliance for iOS releases", Body: boilerplate, ContentHash: "b"},
		{Name: "fastmail", Description: "Read and send Fastmail email and calendar data", Body: boilerplate, ContentHash: "c"},
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 0 {
		t.Fatalf("boilerplate body terms created false overlaps: %#v", findingsOfType(findings, FindingOverlap))
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

func TestAnalyzeWithLLMAddsSemanticOverlapFindings(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "ios-review", Description: "review App Store policy compliance", ContentHash: "a"},
		{Name: "app-submission", Description: "prepare app submission metadata", ContentHash: "b"},
	}
	findings, err := AnalyzeWithLLM(context.Background(), skills, Options{LLMAnalyzer: fakeAnalyzer{overlaps: []llm.SemanticOverlap{{SkillNames: []string{"ios-review", "app-submission"}, Reason: "both support App Store release readiness", Provider: "test", Model: "fake"}}}})
	if err != nil {
		t.Fatal(err)
	}
	assertHasType(t, findings, FindingOverlap)
	for _, finding := range findingsOfType(findings, FindingOverlap) {
		if finding.ID == "llm-overlap:app-submission:ios-review" && finding.Reasons[0] != "LLM-assisted semantic overlap: both support App Store release readiness (test/fake)" {
			t.Fatalf("unexpected LLM overlap reason: %#v", finding.Reasons)
		}
	}
}

func TestAnalyzeWithLLMMergesDuplicateOverlapPairs(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "agent-browser", Description: "browser automation screenshots navigation", ContentHash: "a"},
		{Name: "playwriter", Description: "browser automation screenshots navigation", ContentHash: "b"},
	}
	findings, err := AnalyzeWithLLM(context.Background(), skills, Options{LLMAnalyzer: fakeAnalyzer{overlaps: []llm.SemanticOverlap{{SkillNames: []string{"agent-browser", "playwriter"}, Reason: "both automate browsers", Provider: "test", Model: "fake"}}}})
	if err != nil {
		t.Fatal(err)
	}
	overlaps := findingsOfType(findings, FindingOverlap)
	if len(overlaps) != 1 {
		t.Fatalf("expected merged overlap finding, got %#v", overlaps)
	}
	if len(overlaps[0].Reasons) != 2 || !strings.Contains(overlaps[0].Reasons[1], "LLM-assisted semantic overlap") {
		t.Fatalf("expected deterministic and LLM reasons to merge: %#v", overlaps[0].Reasons)
	}
}

func TestOverlapClustersDenseConnectedComponents(t *testing.T) {
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

func TestOverlapDoesNotCreateBroadTransitiveBridgeClusters(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "browser", Description: "Browser automation screenshots pages", ContentHash: "a"},
		{Name: "ui-kit", Description: "Browser automation screenshots pages React native guidelines", ContentHash: "b"},
		{Name: "app-review", Description: "React native guidelines compliance", ContentHash: "c"},
	}
	findings := Analyze(skills, Options{})
	if countType(findings, FindingOverlap) != 2 {
		t.Fatalf("expected sparse bridge to split into pair findings, got %#v", findings)
	}
	for _, finding := range findingsOfType(findings, FindingOverlap) {
		if len(finding.Skills) != 2 {
			t.Fatalf("expected pair finding instead of broad transitive cluster: %#v", finding)
		}
	}
}

type fakeAnalyzer struct {
	overlaps []llm.SemanticOverlap
}

func (a fakeAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (llm.GeneratedSummary, error) {
	return llm.GeneratedSummary{Name: name, Summary: deterministicSummary, Provider: "test", Model: "fake", ContentHash: contentHash}, nil
}

func (a fakeAnalyzer) FindOverlaps(ctx context.Context, summaries []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	return a.overlaps, nil
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

func findingsOfType(findings []Finding, typ FindingType) []Finding {
	var filtered []Finding
	for _, finding := range findings {
		if finding.Type == typ {
			filtered = append(filtered, finding)
		}
	}
	return filtered
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
