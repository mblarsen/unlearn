package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestGeminiAnalyzerEvalFixtures(t *testing.T) {
	fixture := loadGeminiEvalFixture(t, "testdata/evals/gemini_responses.toml")

	t.Run("summary", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeGeminiFixtureText(t, w, fixture.Summary.ResponseText)
		}))
		defer server.Close()

		analyzer := GeminiAnalyzer{APIKey: "test-key", Model: fixture.Summary.Model, BaseURL: server.URL, Client: server.Client()}
		summary, err := analyzer.Summarize(context.Background(), fixture.Summary.Name, fixture.Summary.DeterministicSummary, fixture.Summary.ContentHash)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Name != fixture.Summary.Name || summary.Summary != fixture.Summary.ExpectedSummary || summary.ContentHash != fixture.Summary.ContentHash || summary.Provider != "gemini" || summary.Model != fixture.Summary.Model {
			t.Fatalf("unexpected generated summary: %#v", summary)
		}
	})

	t.Run("semantic overlaps", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeGeminiFixtureText(t, w, fixture.Overlap.ResponseText)
		}))
		defer server.Close()

		analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
		overlaps, err := analyzer.FindOverlaps(context.Background(), []GeneratedSummary{
			{Name: "agent-browser", Summary: "browser automation"},
			{Name: "playwriter", Summary: "browser interaction"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(overlaps) != len(fixture.Overlap.ExpectedGroups) {
			t.Fatalf("overlaps = %#v, want %#v", overlaps, fixture.Overlap.ExpectedGroups)
		}
		for i, want := range fixture.Overlap.ExpectedGroups {
			if strings.Join(overlaps[i].SkillNames, ",") != strings.Join(want.SkillNames, ",") || overlaps[i].Reason != want.Reason || overlaps[i].Provider != "gemini" || overlaps[i].Model != "gemini-test" {
				t.Fatalf("overlap %d = %#v, want %#v", i, overlaps[i], want)
			}
		}
	})

	t.Run("invalid semantic overlap JSON", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if attempts >= len(fixture.InvalidOverlap.Responses) {
				t.Fatalf("unexpected retry %d", attempts+1)
			}
			writeGeminiFixtureText(t, w, fixture.InvalidOverlap.Responses[attempts])
			attempts++
		}))
		defer server.Close()

		analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
		overlaps, err := analyzer.FindOverlaps(context.Background(), []GeneratedSummary{{Name: "agent-browser", Summary: "browser automation"}, {Name: "playwriter", Summary: "browser interaction"}})
		if err == nil {
			t.Fatalf("expected invalid JSON error, got overlaps %#v", overlaps)
		}
		if attempts != fixture.InvalidOverlap.ExpectedAttempts {
			t.Fatalf("attempts = %d, want %d", attempts, fixture.InvalidOverlap.ExpectedAttempts)
		}
		if !strings.Contains(err.Error(), fixture.InvalidOverlap.ExpectedErrorContains) {
			t.Fatalf("error = %q, want to contain %q", err.Error(), fixture.InvalidOverlap.ExpectedErrorContains)
		}
	})
}

func TestCachedAnalyzerEvalFixture(t *testing.T) {
	fixture := loadCacheEvalFixture(t, "testdata/evals/cache.toml")
	summaries := fixture.generatedSummaries()

	key, err := overlapCacheKey(summaries)
	if err != nil {
		t.Fatal(err)
	}
	if key != fixture.Expected.OverlapCacheKey {
		t.Fatalf("overlap cache key = %s, want fixture golden %s", key, fixture.Expected.OverlapCacheKey)
	}

	dir := t.TempDir()
	cachedOverlaps := fixture.cachedSemanticOverlaps()
	seedOverlapCache(t, filepath.Join(OverlapCacheDir(dir), key+".json"), cachedOverlaps)
	seedSummaryCache(t, SummaryCachePath(dir, fixture.SummaryCache.ContentHash), fixture.SummaryCache.generated())

	analyzer := NewCachedAnalyzer(dir, failAnalyzer{})
	gotSummary, err := analyzer.Summarize(context.Background(), fixture.SummaryCache.Name, "changed deterministic summary", fixture.SummaryCache.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if gotSummary != fixture.SummaryCache.generated() {
		t.Fatalf("summary cache read = %#v, want %#v", gotSummary, fixture.SummaryCache.generated())
	}

	gotOverlaps, err := analyzer.FindOverlaps(context.Background(), []GeneratedSummary{summaries[1], summaries[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotOverlaps) != len(cachedOverlaps) || gotOverlaps[0].Reason != cachedOverlaps[0].Reason || strings.Join(gotOverlaps[0].SkillNames, ",") != strings.Join(cachedOverlaps[0].SkillNames, ",") {
		t.Fatalf("overlap cache read = %#v, want %#v", gotOverlaps, cachedOverlaps)
	}
}

type geminiEvalFixture struct {
	Summary        summaryResponseFixture `toml:"summary"`
	Overlap        overlapResponseFixture `toml:"overlap"`
	InvalidOverlap invalidOverlapFixture  `toml:"invalid_overlap"`
}

type summaryResponseFixture struct {
	Name                 string `toml:"name"`
	DeterministicSummary string `toml:"deterministic_summary"`
	ContentHash          string `toml:"content_hash"`
	ResponseText         string `toml:"response_text"`
	ExpectedSummary      string `toml:"expected_summary"`
	Model                string `toml:"model"`
}

type overlapResponseFixture struct {
	ResponseText   string                  `toml:"response_text"`
	ExpectedGroups []semanticOverlapRecord `toml:"expected_groups"`
}

type invalidOverlapFixture struct {
	ExpectedAttempts      int      `toml:"expected_attempts"`
	ExpectedErrorContains string   `toml:"expected_error_contains"`
	Responses             []string `toml:"responses"`
}

type cacheEvalFixture struct {
	Summaries      []generatedSummaryRecord `toml:"summaries"`
	Expected       cacheExpectedRecord      `toml:"expected"`
	CachedOverlaps []semanticOverlapRecord  `toml:"cached_overlaps"`
	SummaryCache   generatedSummaryRecord   `toml:"summary_cache"`
}

type cacheExpectedRecord struct {
	OverlapCacheKey string `toml:"overlap_cache_key"`
}

type generatedSummaryRecord struct {
	Name        string `toml:"name"`
	Summary     string `toml:"summary"`
	Provider    string `toml:"provider"`
	Model       string `toml:"model"`
	ContentHash string `toml:"content_hash"`
}

type semanticOverlapRecord struct {
	SkillNames []string `toml:"skill_names"`
	Reason     string   `toml:"reason"`
	Provider   string   `toml:"provider"`
	Model      string   `toml:"model"`
}

func loadGeminiEvalFixture(t *testing.T, path string) geminiEvalFixture {
	t.Helper()
	var fixture geminiEvalFixture
	if _, err := toml.DecodeFile(path, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Summary.ResponseText == "" || len(fixture.Overlap.ExpectedGroups) == 0 || len(fixture.InvalidOverlap.Responses) == 0 {
		t.Fatalf("fixture %s is incomplete: %#v", path, fixture)
	}
	return fixture
}

func loadCacheEvalFixture(t *testing.T, path string) cacheEvalFixture {
	t.Helper()
	var fixture cacheEvalFixture
	if _, err := toml.DecodeFile(path, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Summaries) < 2 || fixture.Expected.OverlapCacheKey == "" || fixture.SummaryCache.ContentHash == "" {
		t.Fatalf("fixture %s is incomplete: %#v", path, fixture)
	}
	return fixture
}

func writeGeminiFixtureText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": []map[string]string{{"text": text}},
			},
		}},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatal(err)
	}
}

func (f cacheEvalFixture) generatedSummaries() []GeneratedSummary {
	summaries := make([]GeneratedSummary, 0, len(f.Summaries))
	for _, summary := range f.Summaries {
		summaries = append(summaries, summary.generated())
	}
	return summaries
}

func (f cacheEvalFixture) cachedSemanticOverlaps() []SemanticOverlap {
	overlaps := make([]SemanticOverlap, 0, len(f.CachedOverlaps))
	for _, overlap := range f.CachedOverlaps {
		overlaps = append(overlaps, overlap.semantic())
	}
	return overlaps
}

func (r generatedSummaryRecord) generated() GeneratedSummary {
	return GeneratedSummary{Name: r.Name, Summary: r.Summary, Provider: r.Provider, Model: r.Model, ContentHash: r.ContentHash}
}

func (r semanticOverlapRecord) semantic() SemanticOverlap {
	return SemanticOverlap{SkillNames: r.SkillNames, Reason: r.Reason, Provider: r.Provider, Model: r.Model}
}

func seedOverlapCache(t *testing.T, path string, overlaps []SemanticOverlap) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(overlapCacheFile{Version: overlapCacheVersion, Overlaps: overlaps}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func seedSummaryCache(t *testing.T, path string, summary GeneratedSummary) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

type failAnalyzer struct{}

func (failAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	return GeneratedSummary{}, errors.New("delegate should not be called for cached summary fixture")
}

func (failAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	return nil, errors.New("delegate should not be called for cached overlap fixture")
}
