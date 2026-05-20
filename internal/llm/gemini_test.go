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
		if r.URL.Path != "/models/gemini-3-flash:generateContent" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatalf("missing API key in query: %s", r.URL.RawQuery)
		}
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req.Contents[0].Parts[0].Text, "Skill name: alpha") {
			t.Fatalf("prompt missing skill name: %#v", req)
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
