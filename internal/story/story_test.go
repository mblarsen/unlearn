package story

import (
	"strings"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestBuildReportsObservedInstallFactsAndUnknownHistory(t *testing.T) {
	story := Build([]inventory.Skill{{
		Name:            "alpha",
		EncounteredPath: "/tmp/skills/α",
		ResolvedPath:    "/tmp/source/α",
		IsSymlink:       true,
		Provenance:      "symlink to /tmp/source/α; pi global skills root",
		ActiveAgents:    []string{"pi", "codex"},
		InactiveAgents:  []string{"claude"},
	}}, CoverageUnknown)

	if story.Name != "alpha" || len(story.Installs) != 1 {
		t.Fatalf("unexpected story: %#v", story)
	}
	install := story.Installs[0]
	if install.Path != "/tmp/skills/α" || install.ResolvedPath != "/tmp/source/α" || !install.Symlink {
		t.Fatalf("lost exact install identity: %#v", install)
	}
	if !strings.Contains(install.AgentAccess, "pi") || !strings.Contains(install.AgentAccess, "claude") {
		t.Fatalf("missing agent access evidence: %q", install.AgentAccess)
	}
	for _, value := range []string{story.Origin, story.InstallDate, story.UpstreamBaseline, story.ModificationStatus, story.Usage.Status} {
		if !strings.Contains(strings.ToLower(value), "unknown") {
			t.Errorf("expected explicit unknown label, got %q", value)
		}
	}
	if !strings.Contains(story.CopyComparisonNote, "No other installed copy") {
		t.Fatalf("single-copy limit missing: %q", story.CopyComparisonNote)
	}
}

func TestBuildComparesInstalledCopiesWithoutClaimingUpstreamModification(t *testing.T) {
	first := inventory.Skill{
		Name: "alpha", EncounteredPath: "/tmp/a/alpha", ResolvedPath: "/tmp/a/alpha",
		Frontmatter: map[string]string{"name": "alpha", "description": "first", "version": "1"},
		Body:        "# Alpha\nFirst body", ContentHash: "1111",
		SupportRefs: []inventory.SupportRef{{Mention: "references/a.md", Tokens: 2}},
	}
	second := inventory.Skill{
		Name: "alpha", EncounteredPath: "/tmp/b/alpha", ResolvedPath: "/tmp/b/alpha",
		Frontmatter: map[string]string{"name": "alpha", "description": "second", "owner": "team"},
		Body:        "# Alpha\nSecond body", ContentHash: "2222",
		SupportRefs: []inventory.SupportRef{{Mention: "references/b.md", Tokens: 3}},
	}

	result := Build([]inventory.Skill{second, first}, CoverageComplete)
	if len(result.Comparisons) != 1 {
		t.Fatalf("expected one comparison: %#v", result.Comparisons)
	}
	comparison := result.Comparisons[0]
	if comparison.LeftPath != first.EncounteredPath || comparison.RightPath != second.EncounteredPath {
		t.Fatalf("comparison order is not deterministic: %#v", comparison)
	}
	joined := strings.Join(comparison.Differences, "\n")
	for _, want := range []string{"SKILL.md body differs", "metadata description differs", "metadata owner only in", "metadata version only in", "support references differ", "effective content differs"} {
		if !strings.Contains(joined, want) {
			t.Errorf("comparison missing %q: %s", want, joined)
		}
	}
	for _, text := range append([]string{result.ModificationStatus, result.CopyComparisonNote}, comparison.Differences...) {
		if strings.Contains(strings.ToLower(text), "modified upstream") || strings.Contains(strings.ToLower(text), "modified from upstream") {
			t.Fatalf("invented upstream relationship: %q", text)
		}
	}
}

func TestBuildAggregatesNameLevelDerivedUsageWithoutAttributingOneCopy(t *testing.T) {
	seen := time.Date(2026, 5, 19, 14, 30, 0, 0, time.UTC)
	result := Build([]inventory.Skill{
		{Name: "alpha", EncounteredPath: "/tmp/a", HistoryEvidence: "medium", HistorySources: []string{"/tmp/history-a"}, HistoryLastSeenAt: seen},
		{Name: "alpha", EncounteredPath: "/tmp/b", HistoryEvidence: "strong", HistorySources: []string{"/tmp/history-a", "/tmp/history-b"}},
	}, CoverageIncomplete)

	if result.Usage.Status != "strong derived evidence" || result.Usage.SourceCount != 2 || !result.Usage.LastSeen.Equal(seen) {
		t.Fatalf("unexpected usage summary: %#v", result.Usage)
	}
	if !strings.Contains(strings.ToLower(result.Usage.Attribution), "skill name") || !strings.Contains(strings.ToLower(result.Usage.Attribution), "not a specific installed copy") {
		t.Fatalf("missing name-level attribution limit: %q", result.Usage.Attribution)
	}
	if !strings.Contains(strings.ToLower(result.Usage.Coverage), "incomplete") {
		t.Fatalf("missing incomplete coverage: %q", result.Usage.Coverage)
	}
}

func TestBuildExplainsNotObservedDoesNotMeanUnused(t *testing.T) {
	result := Build([]inventory.Skill{{Name: "alpha", EncounteredPath: "/tmp/a"}}, CoverageComplete)
	if result.Usage.Status != "not observed" {
		t.Fatalf("unexpected usage status: %q", result.Usage.Status)
	}
	if !strings.Contains(strings.ToLower(result.Usage.Limit), "does not mean unused") {
		t.Fatalf("missing absence limit: %q", result.Usage.Limit)
	}
}
