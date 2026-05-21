package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiAnalyzerSummarizeCallsGenerateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-3-flash-preview:generateContent" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatalf("missing API key in query: %s", r.URL.RawQuery)
		}
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		prompt := req.Contents[0].Parts[0].Text
		if !strings.Contains(prompt, "Skill name: alpha") || !strings.Contains(prompt, "at most 12 words") || !strings.Contains(prompt, "Do not restate the description verbatim") {
			t.Fatalf("prompt missing short-summary constraints: %#v", req)
		}
		if req.GenerationConfig.MaxOutputTokens > 64 {
			t.Fatalf("summary token budget should stay short, got %d", req.GenerationConfig.MaxOutputTokens)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Handles alpha workflows."}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: DefaultGeminiModel, BaseURL: server.URL, Client: server.Client()}
	summary, err := analyzer.Summarize(context.Background(), "alpha", "deterministic", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Name != "alpha" || summary.Summary != "Handles alpha workflows." || summary.Provider != "gemini" || summary.Model != DefaultGeminiModel {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestGeminiAnalyzerSummarizeFallsBackFromVeryLongResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"This response is much too long and keeps explaining the entire skill description instead of returning a compact label for cleanup analysis."}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: DefaultGeminiModel, BaseURL: server.URL, Client: server.Client()}
	summary, err := analyzer.Summarize(context.Background(), "alpha", "release readiness", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary != "release readiness" {
		t.Fatalf("expected short deterministic fallback, got %#v", summary)
	}
}

func TestGeminiAnalyzerFindOverlapsParsesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Fatalf("expected JSON mode, got %#v", req.GenerationConfig)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"overlaps\":[{\"skill_names\":[\"ios-review\",\"app-submission\"],\"reason\":\"both support release readiness\"}]}"}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
	overlaps, err := analyzer.FindOverlaps(context.Background(), []GeneratedSummary{
		{Name: "ios-review", Summary: "review App Store compliance"},
		{Name: "app-submission", Summary: "prepare app submission metadata"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 || overlaps[0].Reason != "both support release readiness" || overlaps[0].Provider != "gemini" || overlaps[0].Model != "gemini-test" {
		t.Fatalf("unexpected overlaps: %#v", overlaps)
	}
}

func TestGeminiAnalyzerFindOverlapsRetriesTruncatedJSON(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		if attempts < 3 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"overlaps\":[{\"skill_names\":[\"a\",\"b\"]"}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"overlaps\":[{\"skill_names\":[\"a\",\"b\"],\"reason\":\"same purpose\"}]}"}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
	overlaps, err := analyzer.FindOverlaps(context.Background(), []GeneratedSummary{{Name: "a", Summary: "first"}, {Name: "b", Summary: "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || len(overlaps) != 1 || overlaps[0].Reason != "same purpose" {
		t.Fatalf("attempts=%d overlaps=%#v", attempts, overlaps)
	}
}

func TestNewGeminiAnalyzerFromEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key")
	t.Setenv("UNLEARN_LLM_MODEL", "gemini-custom")
	t.Setenv("UNLEARN_GEMINI_BASE_URL", "http://example.test")

	analyzer, ok := NewGeminiAnalyzerFromEnv()
	if !ok {
		t.Fatal("expected analyzer")
	}
	if analyzer.APIKey != "google-key" || analyzer.Model != "gemini-custom" || analyzer.BaseURL != "http://example.test" {
		t.Fatalf("unexpected analyzer: %#v", analyzer)
	}
}
