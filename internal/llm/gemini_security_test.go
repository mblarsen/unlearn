package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGeminiKeyOnlyInHeaderAndRedactedFromErrors(t *testing.T) {
	const key = "test-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), key) || r.Header.Get("x-goog-api-key") != key {
			t.Errorf("credential must be header-only")
		}
		http.Error(w, "reflected credential: "+key, http.StatusBadRequest)
	}))
	defer server.Close()
	a := GeminiAnalyzer{APIKey: key, BaseURL: server.URL, Client: server.Client()}
	_, err := a.generateText(context.Background(), "test", 48, false)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("credential leaked or missing error: %v", err)
	}
}

func TestGeminiCancellationRemainsDetectable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := GeminiAnalyzer{APIKey: "secret", BaseURL: "http://127.0.0.1:1"}
	_, err := a.generateText(ctx, "test", 48, false)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe cancellation: %v", err)
	}
}

type deadlineProbeTransport func(*http.Request) (*http.Response, error)

func (f deadlineProbeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeminiDefaultDeadlineAndThinking(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = deadlineProbeTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 110*time.Second || time.Until(deadline) > 120*time.Second {
			t.Error("default request must have bounded two-minute deadline")
		}
		var request geminiGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.GenerationConfig.ThinkingConfig == nil || request.GenerationConfig.ThinkingConfig.ThinkingLevel != "low" {
			t.Error("Gemini 3 review must request low thinking")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"ok"}]}}]}`)), Header: make(http.Header)}, nil
	})
	a := GeminiAnalyzer{APIKey: "secret"}
	if _, err := a.generateText(context.Background(), "test", 48, false); err != nil {
		t.Fatal(err)
	}
}

func TestRedactPersistedDiagnosticAfterKeyRotation(t *testing.T) {
	message := `Post "https://example.test/api?key=old-secret": timeout`
	if got := RedactDiagnostic(message); strings.Contains(got, "old-secret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatal("persisted URL credential not redacted")
	}
}

func TestGeminiRejectsRedirectWithoutMutatingClient(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := source.Client()
	policyCalled := false
	client.CheckRedirect = func(*http.Request, []*http.Request) error { policyCalled = true; return nil }
	a := GeminiAnalyzer{APIKey: "test-secret", BaseURL: source.URL, Client: client}
	_, err := a.generateText(context.Background(), "test", 48, false)
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") || redirected || policyCalled {
		t.Fatalf("redirect not safely refused: err=%v target=%v policy=%v", err, redirected, policyCalled)
	}
	_ = client.CheckRedirect(nil, nil)
	if !policyCalled {
		t.Fatal("injected client policy was mutated")
	}
}
