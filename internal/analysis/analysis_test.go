package analysis

import (
	"testing"

	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestAnalyzeDetectsDuplicateConflictAndBroken(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "same", ID: "a", ContentHash: "h1"},
		{Name: "same", ID: "b", ContentHash: "h1"},
		{Name: "conflict", ID: "c", ContentHash: "h1"},
		{Name: "conflict", ID: "d", ContentHash: "h2"},
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

func assertHasType(t *testing.T, findings []Finding, typ FindingType) {
	t.Helper()
	for _, finding := range findings {
		if finding.Type == typ {
			return
		}
	}
	t.Fatalf("missing finding type %s in %#v", typ, findings)
}
