package llm

import "context"

type GeneratedSummary struct {
	Summary     string
	Provider    string
	Model       string
	ContentHash string
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
