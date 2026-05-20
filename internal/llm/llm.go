package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type GeneratedSummary struct {
	Summary     string `json:"summary"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ContentHash string `json:"content_hash"`
}

type SemanticOverlap struct {
	SkillNames []string
	Reason     string
	Provider   string
	Model      string
}

type ActivationRiskRequest struct {
	Name                 string
	DeterministicRisk    string
	DeterministicSignals []string
	ContentHash          string
}

type ActivationRiskAssessment struct {
	Risk        string
	Signals     []string
	Provider    string
	Model       string
	ContentHash string
}

// ActivationRiskAssessor is the opt-in extension point for a future cached LLM
// pass. Implementations should cache by ContentHash plus provider/model/prompt and
// only run after deterministic assessment leaves a decision worth escalating.
type ActivationRiskAssessor interface {
	AssessActivationRisk(ctx context.Context, request ActivationRiskRequest) (ActivationRiskAssessment, error)
}

type Analyzer interface {
	Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error)
	FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error)
}

type DisabledAnalyzer struct{}

func (DisabledAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	return GeneratedSummary{Summary: deterministicSummary, Provider: "disabled", Model: "disabled", ContentHash: contentHash}, nil
}

func (DisabledAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	return nil, nil
}

func (DisabledAnalyzer) AssessActivationRisk(ctx context.Context, request ActivationRiskRequest) (ActivationRiskAssessment, error) {
	return ActivationRiskAssessment{Risk: request.DeterministicRisk, Signals: request.DeterministicSignals, Provider: "disabled", Model: "disabled", ContentHash: request.ContentHash}, nil
}

type CachedAnalyzer struct {
	Dir  string
	Next Analyzer
}

func NewCachedAnalyzer(dir string, next Analyzer) CachedAnalyzer {
	return CachedAnalyzer{Dir: dir, Next: next}
}

func (a CachedAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	if strings.TrimSpace(contentHash) == "" || a.Dir == "" {
		return a.next().Summarize(ctx, name, deterministicSummary, contentHash)
	}
	path := a.summaryPath(contentHash)
	cached, err := readSummary(path)
	if err == nil && cached.ContentHash == contentHash {
		return cached, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return GeneratedSummary{}, err
	}
	summary, err := a.next().Summarize(ctx, name, deterministicSummary, contentHash)
	if err != nil {
		return GeneratedSummary{}, err
	}
	if summary.ContentHash == "" {
		summary.ContentHash = contentHash
	}
	return summary, writeSummary(path, summary)
}

func (a CachedAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	return a.next().FindOverlaps(ctx, summaries)
}

func (a CachedAnalyzer) next() Analyzer {
	if a.Next == nil {
		return DisabledAnalyzer{}
	}
	return a.Next
}

func (a CachedAnalyzer) summaryPath(contentHash string) string {
	return filepath.Join(a.Dir, safeFileName(contentHash)+".json")
}

func readSummary(path string) (GeneratedSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GeneratedSummary{}, err
	}
	var summary GeneratedSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return GeneratedSummary{}, err
	}
	return summary, nil
}

func writeSummary(path string, summary GeneratedSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func safeFileName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "summary"
	}
	return b.String()
}
