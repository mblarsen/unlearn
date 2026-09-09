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
		if r.Header.Get("x-goog-api-key") != "test-key" || r.URL.RawQuery != "" {
			t.Fatal("API key must be sent only in the header")
		}
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		prompt := req.Contents[0].Parts[0].Text
		if !strings.Contains(prompt, "Skill name: alpha") || !strings.Contains(prompt, "at most 12 words") || !strings.Contains(prompt, "Do not restate the description verbatim") {
			t.Fatalf("prompt missing short-summary constraints: %#v", req)
		}
		if req.GenerationConfig.MaxOutputTokens != 8192 {
			t.Fatalf("thinking model needs answer and reasoning budget, got %d", req.GenerationConfig.MaxOutputTokens)
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

func TestGeminiAnalyzerGenerateMergedSkillDraft(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		prompt := req.Contents[0].Parts[0].Text
		if !strings.Contains(prompt, "preview-only") || !strings.Contains(prompt, "Return only plain markdown") || !strings.Contains(prompt, "alpha") || !strings.Contains(prompt, "beta") {
			t.Fatalf("draft prompt missing constraints or selected skills: %s", prompt)
		}
		if req.GenerationConfig.MaxOutputTokens < 4096 {
			t.Fatalf("draft token budget too small: %d", req.GenerationConfig.MaxOutputTokens)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"---\nname: merged-alpha-beta\ndescription: Combined alpha beta workflows\n---\n\n# Merged"}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
	result, err := analyzer.GenerateMergedSkillDraft(context.Background(), DraftRequest{Skills: []DraftSkill{{Name: "alpha", Body: "alpha body"}, {Name: "beta", Body: "beta body"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, "merged-alpha-beta") || result.Provider != "gemini" || result.Model != "gemini-test" || result.PromptVersion != DraftPromptVersion {
		t.Fatalf("unexpected draft result: %#v", result)
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

func TestGeminiAnalyzerLintSkillQualityParsesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		prompt := req.Contents[0].Parts[0].Text
		if req.GenerationConfig.ResponseMimeType != "application/json" {
			t.Fatalf("expected JSON mode, got %#v", req.GenerationConfig)
		}
		for _, want := range []string{"advisory lint only", "Allowed issue_type values", "overly_broad_trigger", "support_refs"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt missing %q:\n%s", want, prompt)
			}
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"findings\":[{\"issue_type\":\"overly_broad_trigger\",\"reason\":\"trigger catches every coding task\",\"recommendation\":\"limit activation to UI review requests\"}]}"}]}}]}`))
	}))
	defer server.Close()

	analyzer := GeminiAnalyzer{APIKey: "test-key", Model: "gemini-test", BaseURL: server.URL, Client: server.Client()}
	result, err := analyzer.LintSkillQuality(context.Background(), SkillQualityRequest{Name: "alpha", Description: "Use before any task", Body: "Body", ContentHash: "hash-a", SupportRefs: []SkillQualitySupportRef{{Path: "docs/all.md", Tokens: 5000}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "gemini" || result.Model != "gemini-test" || result.ContentHash != "hash-a" || len(result.Issues) != 1 {
		t.Fatalf("unexpected quality result: %#v", result)
	}
	if result.Issues[0].IssueType != "overly_broad_trigger" || result.Issues[0].Provider != "gemini" || result.Issues[0].Model != "gemini-test" {
		t.Fatalf("unexpected quality issue: %#v", result.Issues[0])
	}
}

func TestParseGeminiSkillQualityResponseRejectsInvalidJSON(t *testing.T) {
	if _, err := parseGeminiSkillQualityResponse("not json"); err == nil {
		t.Fatal("expected parse error")
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
