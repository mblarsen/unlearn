package llm

import (
	"context"
	"testing"
)

func TestCachedAnalyzerReusesSummaryByContentHash(t *testing.T) {
	delegate := &countingAnalyzer{}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	ctx := context.Background()

	first, err := analyzer.Summarize(ctx, "skill", "deterministic", "hash/one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := analyzer.Summarize(ctx, "skill", "changed", "hash/one")
	if err != nil {
		t.Fatal(err)
	}
	if delegate.summaryCalls != 1 {
		t.Fatalf("expected delegate once, got %d", delegate.summaryCalls)
	}
	if second.Summary != first.Summary || second.ContentHash != "hash/one" || second.Name != "skill" {
		t.Fatalf("cache miss or malformed summary: first=%#v second=%#v", first, second)
	}
}

func TestCachedAnalyzerReusesOverlapsBySummarySet(t *testing.T) {
	delegate := &countingAnalyzer{overlaps: []SemanticOverlap{{SkillNames: []string{"alpha", "beta"}, Reason: "same task", Provider: "test", Model: "fake"}}}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	ctx := context.Background()
	summaries := []GeneratedSummary{
		{Name: "alpha", Summary: "first task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-a"},
		{Name: "beta", Summary: "second task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-b"},
	}

	first, err := analyzer.FindOverlaps(ctx, summaries)
	if err != nil {
		t.Fatal(err)
	}
	second, err := analyzer.FindOverlaps(ctx, summaries)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.overlapCalls != 1 {
		t.Fatalf("expected delegate once, got %d", delegate.overlapCalls)
	}
	if len(first) != 1 || len(second) != 1 || second[0].Reason != "same task" {
		t.Fatalf("unexpected cached overlaps: first=%#v second=%#v", first, second)
	}
}

func TestCachedAnalyzerCachesEmptyOverlapResults(t *testing.T) {
	delegate := &countingAnalyzer{overlaps: []SemanticOverlap{}}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	summaries := []GeneratedSummary{
		{Name: "alpha", Summary: "first task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-a"},
		{Name: "beta", Summary: "second task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-b"},
	}

	for i := 0; i < 2; i++ {
		overlaps, err := analyzer.FindOverlaps(context.Background(), summaries)
		if err != nil {
			t.Fatal(err)
		}
		if len(overlaps) != 0 {
			t.Fatalf("expected no overlaps, got %#v", overlaps)
		}
	}
	if delegate.overlapCalls != 1 {
		t.Fatalf("expected empty result to be cached, delegate calls=%d", delegate.overlapCalls)
	}
}

func TestCachedAnalyzerOverlapCacheInvalidatesOnContentHashChange(t *testing.T) {
	delegate := &countingAnalyzer{}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	ctx := context.Background()
	base := []GeneratedSummary{
		{Name: "alpha", Summary: "first task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-a"},
		{Name: "beta", Summary: "second task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-b"},
	}
	changed := append([]GeneratedSummary(nil), base...)
	changed[1].ContentHash = "hash-c"

	if _, err := analyzer.FindOverlaps(ctx, base); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzer.FindOverlaps(ctx, changed); err != nil {
		t.Fatal(err)
	}
	if delegate.overlapCalls != 2 {
		t.Fatalf("expected content hash change to miss cache, delegate calls=%d", delegate.overlapCalls)
	}
}

func TestCachedAnalyzerOverlapCacheKeyIsOrderIndependent(t *testing.T) {
	delegate := &countingAnalyzer{overlaps: []SemanticOverlap{{SkillNames: []string{"alpha", "beta"}, Reason: "same task"}}}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	ctx := context.Background()
	summaries := []GeneratedSummary{
		{Name: "alpha", Summary: "first task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-a"},
		{Name: "beta", Summary: "second task", Provider: "gemini", Model: "gemini-test", ContentHash: "hash-b"},
	}
	reversed := []GeneratedSummary{summaries[1], summaries[0]}

	if _, err := analyzer.FindOverlaps(ctx, summaries); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzer.FindOverlaps(ctx, reversed); err != nil {
		t.Fatal(err)
	}
	if delegate.overlapCalls != 1 {
		t.Fatalf("expected order-independent overlap cache key, delegate calls=%d", delegate.overlapCalls)
	}
}

func TestCachedAnalyzerReusesSkillQualityByContentHashProviderModelAndVersion(t *testing.T) {
	delegate := &countingAnalyzer{quality: SkillQualityResult{Issues: []SkillQualityIssue{{IssueType: "vague_description", Reason: "too broad", Recommendation: "be specific"}}, Provider: "test", Model: "fake"}}
	analyzer := NewCachedAnalyzer(t.TempDir(), delegate)
	ctx := context.Background()
	request := SkillQualityRequest{Name: "alpha", Description: "first", ContentHash: "hash-a"}

	first, err := analyzer.LintSkillQuality(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Description = "changed"
	second, err := analyzer.LintSkillQuality(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.qualityCalls != 1 {
		t.Fatalf("expected delegate once, got %d", delegate.qualityCalls)
	}
	if len(first.Issues) != 1 || len(second.Issues) != 1 || second.ContentHash != "hash-a" {
		t.Fatalf("unexpected cached quality results: first=%#v second=%#v", first, second)
	}
}

func TestCachedAnalyzerQualityCacheInvalidatesOnProviderModelChange(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	firstDelegate := &countingAnalyzer{provider: "test", model: "one"}
	secondDelegate := &countingAnalyzer{provider: "test", model: "two"}

	if _, err := NewCachedAnalyzer(dir, firstDelegate).LintSkillQuality(ctx, SkillQualityRequest{Name: "alpha", ContentHash: "hash-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCachedAnalyzer(dir, secondDelegate).LintSkillQuality(ctx, SkillQualityRequest{Name: "alpha", ContentHash: "hash-a"}); err != nil {
		t.Fatal(err)
	}
	if firstDelegate.qualityCalls != 1 || secondDelegate.qualityCalls != 1 {
		t.Fatalf("expected provider/model change to miss cache, first=%d second=%d", firstDelegate.qualityCalls, secondDelegate.qualityCalls)
	}
}

func TestCachedDraftGeneratorReusesDraftBySelectedContentAndModel(t *testing.T) {
	delegate := &countingDraftGenerator{markdown: "---\nname: merged\n---\n\n# Merged"}
	generator := NewCachedDraftGenerator(t.TempDir(), delegate)
	request := DraftRequest{PromptVersion: DraftPromptVersion, Skills: []DraftSkill{
		{Name: "alpha", ContentHash: "hash-a", Body: "alpha"},
		{Name: "beta", ContentHash: "hash-b", Body: "beta"},
	}}
	reversed := DraftRequest{PromptVersion: DraftPromptVersion, Skills: []DraftSkill{request.Skills[1], request.Skills[0]}}

	first, err := generator.GenerateMergedSkillDraft(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.GenerateMergedSkillDraft(context.Background(), reversed)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.calls != 1 {
		t.Fatalf("expected delegate once, got %d", delegate.calls)
	}
	if second.Markdown != first.Markdown || second.Provider != "test" || second.Model != "fake" {
		t.Fatalf("unexpected cached draft: first=%#v second=%#v", first, second)
	}
}

func TestCachedDraftGeneratorInvalidatesOnContentHashChange(t *testing.T) {
	delegate := &countingDraftGenerator{markdown: "draft"}
	generator := NewCachedDraftGenerator(t.TempDir(), delegate)
	base := DraftRequest{PromptVersion: DraftPromptVersion, Skills: []DraftSkill{{Name: "alpha", ContentHash: "hash-a"}, {Name: "beta", ContentHash: "hash-b"}}}
	changed := DraftRequest{PromptVersion: DraftPromptVersion, Skills: []DraftSkill{{Name: "alpha", ContentHash: "hash-a"}, {Name: "beta", ContentHash: "hash-c"}}}
	if _, err := generator.GenerateMergedSkillDraft(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if _, err := generator.GenerateMergedSkillDraft(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if delegate.calls != 2 {
		t.Fatalf("expected changed content hash to miss cache, calls=%d", delegate.calls)
	}
}

type countingAnalyzer struct {
	summaryCalls int
	overlapCalls int
	qualityCalls int
	overlaps     []SemanticOverlap
	quality      SkillQualityResult
	provider     string
	model        string
}

func (a *countingAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	a.summaryCalls++
	return GeneratedSummary{Name: name, Summary: deterministicSummary, Provider: "test", Model: "fake", ContentHash: contentHash}, nil
}

func (a *countingAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	a.overlapCalls++
	return append([]SemanticOverlap(nil), a.overlaps...), nil
}

func (a *countingAnalyzer) LintSkillQuality(ctx context.Context, request SkillQualityRequest) (SkillQualityResult, error) {
	a.qualityCalls++
	result := a.quality
	if result.Provider == "" {
		result.Provider = a.ProviderName()
	}
	if result.Model == "" {
		result.Model = a.ModelName()
	}
	result.ContentHash = request.ContentHash
	if result.Issues == nil {
		result.Issues = []SkillQualityIssue{}
	}
	return result, nil
}

func (a *countingAnalyzer) ProviderName() string {
	if a.provider != "" {
		return a.provider
	}
	return "test"
}

func (a *countingAnalyzer) ModelName() string {
	if a.model != "" {
		return a.model
	}
	return "fake"
}

type countingDraftGenerator struct {
	calls    int
	markdown string
	provider string
	model    string
}

func (g *countingDraftGenerator) GenerateMergedSkillDraft(ctx context.Context, request DraftRequest) (DraftResult, error) {
	g.calls++
	provider := g.ProviderName()
	model := g.ModelName()
	return DraftResult{Markdown: g.markdown, Provider: provider, Model: model, PromptVersion: request.PromptVersion}, nil
}

func (g *countingDraftGenerator) ProviderName() string {
	if g.provider != "" {
		return g.provider
	}
	return "test"
}

func (g *countingDraftGenerator) ModelName() string {
	if g.model != "" {
		return g.model
	}
	return "fake"
}
