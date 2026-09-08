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
		"Create a short cleanup label for this AI agent skill.",
		"Return at most 12 words as one short phrase; no full sentence is required.",
		"Focus on the user task the skill helps with.",
		"Do not restate the description verbatim. Do not use markdown.",
		"Return only the phrase.",
		"Skill name: " + name,
		"Deterministic summary: " + deterministicSummary,
	}, "\n")
	text, err := a.generateText(ctx, prompt, 48, false)
	if err != nil {
		return GeneratedSummary{}, err
	}
	text = firstLine(strings.TrimSpace(text))
	if text == "" {
		text = deterministicSummary
	} else if summaryTooLong(text) && !summaryTooLong(deterministicSummary) && strings.TrimSpace(deterministicSummary) != "" {
		text = firstLine(strings.TrimSpace(deterministicSummary))
	}
	return GeneratedSummary{Name: name, Summary: text, Provider: "gemini", Model: a.model(), ContentHash: contentHash}, nil
}

func (a GeminiAnalyzer) ProviderName() string { return "gemini" }

func (a GeminiAnalyzer) ModelName() string { return a.model() }

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
	var decoded geminiOverlapResponse
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		text, err := a.generateText(ctx, prompt, 4096, true)
		if err != nil {
			return nil, err
		}
		decoded, err = parseGeminiOverlapResponse(text)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("Gemini returned invalid semantic-overlap JSON after 3 attempts: %w", lastErr)
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

func (a GeminiAnalyzer) LintSkillQuality(ctx context.Context, request SkillQualityRequest) (SkillQualityResult, error) {
	payload := map[string]any{
		"name":         request.Name,
		"description":  request.Description,
		"body_preview": truncatePromptText(request.Body, 6000),
		"support_refs": request.SupportRefs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return SkillQualityResult{}, err
	}
	prompt := strings.Join([]string{
		"Review one AI agent skill file for quality risks. This is advisory lint only; do not recommend deletion, quarantine, or renaming.",
		"Report only concrete issues that make the skill risky or hard to use correctly.",
		"Allowed issue_type values: vague_description, overly_broad_trigger, conflicting_instructions, missing_examples, unnecessary_support_file_loading.",
		"Prefer no findings when the skill is clear enough. Return at most 3 findings.",
		"Return strict JSON with this shape: {\"findings\":[{\"issue_type\":\"vague_description\",\"reason\":\"short concrete reason\",\"recommendation\":\"specific preview-only improvement\"}]}",
		"Skill JSON:",
		string(data),
	}, "\n")
	var decoded geminiSkillQualityResponse
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		text, err := a.generateText(ctx, prompt, 2048, true)
		if err != nil {
			return SkillQualityResult{}, err
		}
		decoded, err = parseGeminiSkillQualityResponse(text)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return SkillQualityResult{}, fmt.Errorf("Gemini returned invalid skill-quality JSON after 3 attempts: %w", lastErr)
	}
	issues := make([]SkillQualityIssue, 0, len(decoded.Findings))
	for _, item := range decoded.Findings {
		issueType := strings.TrimSpace(item.IssueType)
		reason := strings.TrimSpace(item.Reason)
		recommendation := strings.TrimSpace(item.Recommendation)
		if issueType == "" || reason == "" || recommendation == "" {
			continue
		}
		issues = append(issues, SkillQualityIssue{IssueType: issueType, Reason: reason, Recommendation: recommendation, Provider: "gemini", Model: a.model()})
		if len(issues) == 3 {
			break
		}
	}
	return SkillQualityResult{Issues: issues, Provider: "gemini", Model: a.model(), ContentHash: request.ContentHash}, nil
}

func (a GeminiAnalyzer) GenerateMergedSkillDraft(ctx context.Context, request DraftRequest) (DraftResult, error) {
	if len(request.Skills) < 2 {
		return DraftResult{}, fmt.Errorf("select at least two skills to draft a merge")
	}
	if request.PromptVersion == "" {
		request.PromptVersion = DraftPromptVersion
	}
	items := make([]map[string]any, 0, len(request.Skills))
	for _, skill := range request.Skills {
		items = append(items, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"frontmatter": skill.Frontmatter,
			"body":        skill.Body,
			"path":        skill.Path,
		})
	}
	data, err := json.Marshal(items)
	if err != nil {
		return DraftResult{}, err
	}
	prompt := strings.Join([]string{
		"Draft a proposed merged SKILL.md for the selected AI-agent skills.",
		"This is preview-only: do not describe filesystem actions, renames, deletion, quarantine, or installation steps.",
		"Return only plain markdown for one complete SKILL.md file, including YAML frontmatter and the body.",
		"The frontmatter must include a concise kebab-case name and a description that preserves the useful trigger intent from the inputs without becoming overly broad.",
		"Preserve critical safety constraints, workflow steps, command examples, and relative references only when they still make sense for one combined skill.",
		"Remove duplicate or contradictory wording. Prefer clear sections and concise instructions.",
		"Selected skills JSON:",
		string(data),
	}, "\n")
	text, err := a.generateText(ctx, prompt, 8192, false)
	if err != nil {
		return DraftResult{}, err
	}
	markdown := strings.TrimSpace(text)
	if markdown == "" {
		return DraftResult{}, fmt.Errorf("Gemini returned an empty merged skill draft")
	}
	return DraftResult{Markdown: markdown, Provider: "gemini", Model: a.model(), PromptVersion: request.PromptVersion}, nil
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

type geminiOverlapResponse struct {
	Overlaps []struct {
		SkillNames []string `json:"skill_names"`
		Reason     string   `json:"reason"`
	} `json:"overlaps"`
}

type geminiSkillQualityResponse struct {
	Findings []struct {
		IssueType      string `json:"issue_type"`
		Reason         string `json:"reason"`
		Recommendation string `json:"recommendation"`
	} `json:"findings"`
}

func parseGeminiOverlapResponse(text string) (geminiOverlapResponse, error) {
	jsonText := extractJSONObject(text)
	if strings.TrimSpace(jsonText) == "" {
		return geminiOverlapResponse{}, fmt.Errorf("no JSON object in response preview %q", truncateForError(text, 240))
	}
	var decoded geminiOverlapResponse
	if err := json.Unmarshal([]byte(jsonText), &decoded); err != nil {
		return geminiOverlapResponse{}, fmt.Errorf("could not parse JSON (%w); response preview %q", err, truncateForError(text, 240))
	}
	return decoded, nil
}

func parseGeminiSkillQualityResponse(text string) (geminiSkillQualityResponse, error) {
	jsonText := extractJSONObject(text)
	if strings.TrimSpace(jsonText) == "" {
		return geminiSkillQualityResponse{}, fmt.Errorf("no JSON object in response preview %q", truncateForError(text, 240))
	}
	var decoded geminiSkillQualityResponse
	if err := json.Unmarshal([]byte(jsonText), &decoded); err != nil {
		return geminiSkillQualityResponse{}, fmt.Errorf("could not parse JSON (%w); response preview %q", err, truncateForError(text, 240))
	}
	return decoded, nil
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

func summaryTooLong(value string) bool {
	value = strings.TrimSpace(value)
	return len([]rune(value)) > 96 || len(strings.Fields(value)) > 16
}

func truncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func truncatePromptText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
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
