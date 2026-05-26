package analysis

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

func TestAnalyzeWithLLMEvalFixture(t *testing.T) {
	fixture := loadAnalysisLLMEvalFixture(t, "testdata/llm_evals/semantic_overlap.toml")
	analyzer := newFixtureLLMAnalyzer(t, fixture)

	findings, err := AnalyzeWithLLM(context.Background(), fixture.skills(), Options{LLMAnalyzer: analyzer})
	if err != nil {
		t.Fatal(err)
	}

	analyzer.assertSummariesSeen()

	for _, want := range fixture.ExpectedOverlapFindings {
		finding := findingByID(findings, want.ID)
		if finding == nil {
			t.Fatalf("missing expected LLM overlap finding %q in %#v", want.ID, findingsOfType(findings, FindingOverlap))
		}
		if finding.Type != FindingOverlap || finding.Severity != 2 {
			t.Fatalf("LLM overlap should remain a non-destructive overlap finding, got %#v", finding)
		}
		if got := sortedFindingSkillNames(*finding); !reflect.DeepEqual(got, sortedStrings(want.SkillNames)) {
			t.Fatalf("finding %s skills = %#v, want %#v", want.ID, got, sortedStrings(want.SkillNames))
		}
		if len(finding.Reasons) != 1 || finding.Reasons[0] != want.Reason || !strings.HasPrefix(finding.Reasons[0], "LLM-assisted semantic overlap:") {
			t.Fatalf("finding %s reason = %#v, want labelled reason %q", want.ID, finding.Reasons, want.Reason)
		}
	}

	for _, pair := range fixture.ExpectedNonOverlapPairs {
		if overlapFindingContainsAll(findings, pair.SkillNames) {
			t.Fatalf("unexpected overlap for non-overlap eval pair %#v in %#v", pair.SkillNames, findingsOfType(findings, FindingOverlap))
		}
	}
}

type analysisLLMEvalFixture struct {
	Title                   string                          `toml:"title"`
	Provider                string                          `toml:"provider"`
	Model                   string                          `toml:"model"`
	Skills                  []analysisSkillFixture          `toml:"skills"`
	ExpectedSummaries       []expectedSummaryFixture        `toml:"expected_summaries"`
	LLMOverlaps             []semanticOverlapFixture        `toml:"llm_overlaps"`
	ExpectedOverlapFindings []expectedOverlapFindingFixture `toml:"expected_overlap_findings"`
	ExpectedNonOverlapPairs []skillNamePairFixture          `toml:"expected_non_overlap_pairs"`
}

type analysisSkillFixture struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Body        string `toml:"body"`
	ContentHash string `toml:"content_hash"`
}

type expectedSummaryFixture struct {
	Name                 string `toml:"name"`
	DeterministicSummary string `toml:"deterministic_summary"`
	Summary              string `toml:"summary"`
}

type semanticOverlapFixture struct {
	SkillNames []string `toml:"skill_names"`
	Reason     string   `toml:"reason"`
}

type expectedOverlapFindingFixture struct {
	ID         string   `toml:"id"`
	SkillNames []string `toml:"skill_names"`
	Reason     string   `toml:"reason"`
}

type skillNamePairFixture struct {
	SkillNames []string `toml:"skill_names"`
}

func loadAnalysisLLMEvalFixture(t *testing.T, path string) analysisLLMEvalFixture {
	t.Helper()
	var fixture analysisLLMEvalFixture
	if _, err := toml.DecodeFile(path, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Skills) == 0 || len(fixture.ExpectedSummaries) == 0 {
		t.Fatalf("fixture %s must include skills and expected summaries", path)
	}
	return fixture
}

func (f analysisLLMEvalFixture) skills() []inventory.Skill {
	skills := make([]inventory.Skill, 0, len(f.Skills))
	for _, skill := range f.Skills {
		skills = append(skills, inventory.Skill{
			Name:        skill.Name,
			Description: skill.Description,
			Body:        skill.Body,
			ContentHash: skill.ContentHash,
		})
	}
	return skills
}

type fixtureLLMAnalyzer struct {
	t         *testing.T
	fixture   analysisLLMEvalFixture
	summaries map[string]expectedSummaryFixture
	seen      map[string]llm.GeneratedSummary
}

func newFixtureLLMAnalyzer(t *testing.T, fixture analysisLLMEvalFixture) *fixtureLLMAnalyzer {
	summaries := map[string]expectedSummaryFixture{}
	for _, summary := range fixture.ExpectedSummaries {
		summaries[strings.ToLower(summary.Name)] = summary
	}
	return &fixtureLLMAnalyzer{t: t, fixture: fixture, summaries: summaries, seen: map[string]llm.GeneratedSummary{}}
}

func (a *fixtureLLMAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (llm.GeneratedSummary, error) {
	a.t.Helper()
	want, ok := a.summaries[strings.ToLower(name)]
	if !ok {
		return llm.GeneratedSummary{}, fmt.Errorf("unexpected summary request for %q", name)
	}
	if deterministicSummary != want.DeterministicSummary {
		return llm.GeneratedSummary{}, fmt.Errorf("deterministic summary for %s = %q, want %q", name, deterministicSummary, want.DeterministicSummary)
	}
	summary := llm.GeneratedSummary{Name: name, Summary: want.Summary, Provider: a.fixture.Provider, Model: a.fixture.Model, ContentHash: contentHash}
	a.seen[strings.ToLower(name)] = summary
	return summary, nil
}

func (a *fixtureLLMAnalyzer) FindOverlaps(ctx context.Context, summaries []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	a.t.Helper()
	for _, summary := range summaries {
		want, ok := a.summaries[strings.ToLower(summary.Name)]
		if !ok {
			return nil, fmt.Errorf("unexpected summary passed to overlap analyzer: %#v", summary)
		}
		if summary.Summary != want.Summary || summary.Provider != a.fixture.Provider || summary.Model != a.fixture.Model {
			return nil, fmt.Errorf("summary for %s = %#v, want summary %q from %s/%s", summary.Name, summary, want.Summary, a.fixture.Provider, a.fixture.Model)
		}
	}

	overlaps := make([]llm.SemanticOverlap, 0, len(a.fixture.LLMOverlaps))
	for _, overlap := range a.fixture.LLMOverlaps {
		overlaps = append(overlaps, llm.SemanticOverlap{SkillNames: overlap.SkillNames, Reason: overlap.Reason, Provider: a.fixture.Provider, Model: a.fixture.Model})
	}
	return overlaps, nil
}

func (a *fixtureLLMAnalyzer) assertSummariesSeen() {
	a.t.Helper()
	for name := range a.summaries {
		if _, ok := a.seen[name]; !ok {
			a.t.Fatalf("expected summary request for %s; saw %#v", name, a.seen)
		}
	}
}

func findingByID(findings []Finding, id string) *Finding {
	for i := range findings {
		if findings[i].ID == id {
			return &findings[i]
		}
	}
	return nil
}

func sortedFindingSkillNames(finding Finding) []string {
	names := make([]string, 0, len(finding.Skills))
	for _, skill := range finding.Skills {
		names = append(names, skill.Name)
	}
	return sortedStrings(names)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func overlapFindingContainsAll(findings []Finding, names []string) bool {
	want := map[string]bool{}
	for _, name := range names {
		want[strings.ToLower(name)] = true
	}
	for _, finding := range findingsOfType(findings, FindingOverlap) {
		got := map[string]bool{}
		for _, skill := range finding.Skills {
			got[strings.ToLower(skill.Name)] = true
		}
		containsAll := true
		for name := range want {
			if !got[name] {
				containsAll = false
			}
		}
		if containsAll {
			return true
		}
	}
	return false
}
