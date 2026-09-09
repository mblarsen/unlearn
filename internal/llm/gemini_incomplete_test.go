package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGeminiReportsIncompleteCandidate(t *testing.T) {
	for _, text := range []string{"", `{"findings":[]}`} {
		t.Run(text, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if text == "" {
					_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[]}}]}`))
				} else {
					_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"{\"findings\":[]}"}]}}]}`))
				}
			}))
			defer server.Close()
			a := GeminiAnalyzer{APIKey: "test", BaseURL: server.URL, Client: server.Client()}
			_, err := a.LintSkillQuality(context.Background(), SkillQualityRequest{Name: "demo"})
			if err == nil || !strings.Contains(err.Error(), "MAX_TOKENS") {
				t.Fatalf("wanted explicit truncation error; got %v", err)
			}
		})
	}
}

func TestGeminiRetriesTruncationWithLargerBudget(t *testing.T) {
	var budgets []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		budgets = append(budgets, request.GenerationConfig.MaxOutputTokens)
		if len(budgets) < 3 {
			_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"thought":true,"text":"not answer JSON"},{"text":"{\"findings\":"},{"text":"[]}"}]}}]}`))
	}))
	defer server.Close()
	a := GeminiAnalyzer{APIKey: "test", BaseURL: server.URL, Client: server.Client()}
	_, err := a.LintSkillQuality(context.Background(), SkillQualityRequest{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(budgets, []int{8192, 16384, 32768}) {
		t.Fatalf("budgets %v", budgets)
	}
}

func TestGeminiDoesNotRetrySafetyBlock(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`))
	}))
	defer server.Close()
	a := GeminiAnalyzer{APIKey: "test", BaseURL: server.URL, Client: server.Client()}
	_, err := a.LintSkillQuality(context.Background(), SkillQualityRequest{Name: "demo"})
	if err == nil || !strings.Contains(err.Error(), "SAFETY") || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
