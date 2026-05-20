package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultGeminiModel   = "gemini-3-flash-preview"
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

type GeminiAnalyzer struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

func NewGeminiAnalyzerFromEnv() (GeminiAnalyzer, bool) {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
	}
	if key == "" {
		return GeminiAnalyzer{}, false
	}
	model := strings.TrimSpace(os.Getenv("UNLEARN_LLM_MODEL"))
	if model == "" {
		model = DefaultGeminiModel
	}
	baseURL := strings.TrimSpace(os.Getenv("UNLEARN_GEMINI_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultGeminiBaseURL
	}
	return GeminiAnalyzer{APIKey: key, Model: model, BaseURL: baseURL}, true
}

func (a GeminiAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	prompt := strings.Join([]string{
		"Summarize this AI agent skill for cleanup analysis.",
		"Return one concise sentence. Do not use markdown.",
		"Skill name: " + name,
		"Deterministic summary: " + deterministicSummary,
	}, "\n")
	text, err := a.generateText(ctx, prompt, 160, false)
	if err != nil {
		return GeneratedSummary{}, err
	}
	text = firstLine(strings.TrimSpace(text))
	if text == "" {
		text = deterministicSummary
	}
	return GeneratedSummary{Name: name, Summary: text, Provider: "gemini", Model: a.model(), ContentHash: contentHash}, nil
}

func (a GeminiAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	if len(summaries) < 2 {
		return nil, nil
	}
	items := make([]map[string]string, 0, len(summaries))
	for _, summary := range summaries {
		items = append(items, map[string]string{
			"name":    summaryName(summary),
			"summary": summary.Summary,
		})
	}
	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	prompt := strings.Join([]string{
		"Find semantic overlap between AI agent skills.",
		"Only report groups where the skills meaningfully solve the same user need or should likely be consolidated.",
		"Do not report weak keyword-only overlap.",
		"Return strict JSON with this shape: {\"overlaps\":[{\"skill_names\":[\"name-a\",\"name-b\"],\"reason\":\"short concrete reason\"}]}",
		"Use skill names exactly as provided. If no meaningful overlaps exist, return {\"overlaps\":[]}.",
		"Skills JSON:",
		string(data),
	}, "\n")
	text, err := a.generateText(ctx, prompt, 4096, true)
	if err != nil {
		return nil, err
	}
	jsonText := extractJSONObject(text)
	if strings.TrimSpace(jsonText) == "" {
		return nil, fmt.Errorf("gemini overlap response did not contain JSON: %q", truncateForError(text, 240))
	}
	var decoded struct {
		Overlaps []struct {
			SkillNames []string `json:"skill_names"`
			Reason     string   `json:"reason"`
		} `json:"overlaps"`
	}
	if err := json.Unmarshal([]byte(jsonText), &decoded); err != nil {
		return nil, fmt.Errorf("parse gemini overlap JSON: %w; response: %q", err, truncateForError(text, 240))
	}
	overlaps := make([]SemanticOverlap, 0, len(decoded.Overlaps))
	for _, item := range decoded.Overlaps {
		if len(item.SkillNames) < 2 {
			continue
		}
		overlaps = append(overlaps, SemanticOverlap{SkillNames: item.SkillNames, Reason: item.Reason, Provider: "gemini", Model: a.model()})
	}
	return overlaps, nil
}

func (a GeminiAnalyzer) generateText(ctx context.Context, prompt string, maxTokens int, jsonMode bool) (string, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return "", fmt.Errorf("missing Gemini API key")
	}
	body := geminiGenerateRequest{
		Contents: []geminiContent{{Role: "user", Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: geminiGenerationConfig{
			Temperature:     ptrFloat64(0),
			MaxOutputTokens: maxTokens,
		},
	}
	if jsonMode {
		body.GenerationConfig.ResponseMimeType = "application/json"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini API %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var decoded geminiGenerateResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", err
	}
	for _, candidate := range decoded.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return part.Text, nil
			}
		}
	}
	return "", fmt.Errorf("gemini response contained no text")
}

func (a GeminiAnalyzer) endpoint() string {
	base := strings.TrimRight(a.BaseURL, "/")
	if base == "" {
		base = defaultGeminiBaseURL
	}
	model := strings.TrimPrefix(a.model(), "models/")
	return fmt.Sprintf("%s/models/%s:generateContent?key=%s", base, url.PathEscape(model), url.QueryEscape(a.APIKey))
}

func (a GeminiAnalyzer) model() string {
	model := strings.TrimSpace(a.Model)
	if model == "" {
		return DefaultGeminiModel
	}
	return model
}

func summaryName(summary GeneratedSummary) string {
	return strings.TrimSpace(summary.Name)
}

type geminiGenerateRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

func ptrFloat64(value float64) *float64 { return &value }

func firstLine(value string) string {
	if idx := strings.IndexAny(value, "\r\n"); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

func truncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}
