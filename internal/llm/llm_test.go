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
	if delegate.calls != 1 {
		t.Fatalf("expected delegate once, got %d", delegate.calls)
	}
	if second.Summary != first.Summary || second.ContentHash != "hash/one" {
		t.Fatalf("cache miss or malformed summary: first=%#v second=%#v", first, second)
	}
}

type countingAnalyzer struct {
	calls int
}

func (a *countingAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	a.calls++
	return GeneratedSummary{Summary: deterministicSummary, Provider: "test", Model: "fake", ContentHash: contentHash}, nil
}

func (a *countingAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	return nil, nil
}
