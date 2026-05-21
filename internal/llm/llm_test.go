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

type countingAnalyzer struct {
	summaryCalls int
	overlapCalls int
	overlaps     []SemanticOverlap
}

func (a *countingAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	a.summaryCalls++
	return GeneratedSummary{Name: name, Summary: deterministicSummary, Provider: "test", Model: "fake", ContentHash: contentHash}, nil
}

func (a *countingAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	a.overlapCalls++
	return append([]SemanticOverlap(nil), a.overlaps...), nil
}
