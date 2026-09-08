package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	overlapCacheVersion      = "overlap-v1"
	skillQualityCacheVersion = "skill-quality-v1"
	DraftPromptVersion       = "merged-skill-draft-v1"
)

type GeneratedSummary struct {
	Name        string `json:"name,omitempty"`
	Summary     string `json:"summary"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ContentHash string `json:"content_hash"`
}

type SemanticOverlap struct {
	SkillNames []string `json:"skill_names"`
	Reason     string   `json:"reason"`
	Provider   string   `json:"provider,omitempty"`
	Model      string   `json:"model,omitempty"`
}

type SkillQualitySupportRef struct {
	Mention string `json:"mention,omitempty"`
	Path    string `json:"path,omitempty"`
	Tokens  int    `json:"tokens,omitempty"`
	Broken  bool   `json:"broken,omitempty"`
}

type SkillQualityRequest struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Body        string                   `json:"body"`
	ContentHash string                   `json:"content_hash"`
	SupportRefs []SkillQualitySupportRef `json:"support_refs,omitempty"`
}

type SkillQualityIssue struct {
	IssueType      string `json:"issue_type"`
	Reason         string `json:"reason"`
	Recommendation string `json:"recommendation"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
}

type SkillQualityResult struct {
	Issues      []SkillQualityIssue `json:"issues"`
	Provider    string              `json:"provider"`
	Model       string              `json:"model"`
	ContentHash string              `json:"content_hash"`
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

type DraftSkill struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	Body        string            `json:"body,omitempty"`
	ContentHash string            `json:"content_hash,omitempty"`
	Path        string            `json:"path,omitempty"`
}

type DraftRequest struct {
	Skills        []DraftSkill
	PromptVersion string
}

type DraftResult struct {
	Markdown      string `json:"markdown"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
}

type DraftGenerator interface {
	GenerateMergedSkillDraft(ctx context.Context, request DraftRequest) (DraftResult, error)
}

type IdentifiedGenerator interface {
	ProviderName() string
	ModelName() string
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
	LintSkillQuality(ctx context.Context, request SkillQualityRequest) (SkillQualityResult, error)
}

type ProviderModel interface {
	ProviderName() string
	ModelName() string
}

type DisabledAnalyzer struct{}

func (DisabledAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	return GeneratedSummary{Name: name, Summary: deterministicSummary, Provider: "disabled", Model: "disabled", ContentHash: contentHash}, nil
}

func (DisabledAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	return nil, nil
}

func (DisabledAnalyzer) LintSkillQuality(ctx context.Context, request SkillQualityRequest) (SkillQualityResult, error) {
	return SkillQualityResult{Issues: []SkillQualityIssue{}, Provider: "disabled", Model: "disabled", ContentHash: request.ContentHash}, nil
}

func (DisabledAnalyzer) ProviderName() string { return "disabled" }

func (DisabledAnalyzer) ModelName() string { return "disabled" }

func (DisabledAnalyzer) AssessActivationRisk(ctx context.Context, request ActivationRiskRequest) (ActivationRiskAssessment, error) {
	return ActivationRiskAssessment{Risk: request.DeterministicRisk, Signals: request.DeterministicSignals, Provider: "disabled", Model: "disabled", ContentHash: request.ContentHash}, nil
}

type CachedAnalyzer struct {
	Dir  string
	Next Analyzer
}

type CachedDraftGenerator struct {
	Dir  string
	Next DraftGenerator
}

func NewCachedAnalyzer(dir string, next Analyzer) CachedAnalyzer {
	return CachedAnalyzer{Dir: dir, Next: next}
}

func NewCachedDraftGenerator(dir string, next DraftGenerator) CachedDraftGenerator {
	return CachedDraftGenerator{Dir: dir, Next: next}
}

func (a CachedAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (GeneratedSummary, error) {
	if strings.TrimSpace(contentHash) == "" || a.Dir == "" {
		return a.next().Summarize(ctx, name, deterministicSummary, contentHash)
	}
	path := a.summaryPath(contentHash)
	cached, err := readSummary(path)
	if err == nil && cached.ContentHash == contentHash {
		if cached.Name == "" {
			cached.Name = name
		}
		return cached, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return GeneratedSummary{}, err
	}
	summary, err := a.next().Summarize(ctx, name, deterministicSummary, contentHash)
	if err != nil {
		return GeneratedSummary{}, err
	}
	if summary.Name == "" {
		summary.Name = name
	}
	if summary.ContentHash == "" {
		summary.ContentHash = contentHash
	}
	return summary, writeSummary(path, summary)
}

func (a CachedAnalyzer) FindOverlaps(ctx context.Context, summaries []GeneratedSummary) ([]SemanticOverlap, error) {
	if a.Dir == "" || len(summaries) < 2 {
		return a.next().FindOverlaps(ctx, summaries)
	}
	path, err := a.overlapPath(summaries)
	if err != nil {
		return nil, err
	}
	cached, err := readOverlapCache(path)
	if err == nil && cached.Version == overlapCacheVersion {
		return append([]SemanticOverlap(nil), cached.Overlaps...), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	overlaps, err := a.next().FindOverlaps(ctx, summaries)
	if err != nil {
		return nil, err
	}
	if overlaps == nil {
		overlaps = []SemanticOverlap{}
	}
	return overlaps, writeOverlapCache(path, overlapCacheFile{Version: overlapCacheVersion, Overlaps: overlaps})
}

func (a CachedAnalyzer) LintSkillQuality(ctx context.Context, request SkillQualityRequest) (SkillQualityResult, error) {
	if strings.TrimSpace(request.ContentHash) == "" || a.Dir == "" {
		return a.next().LintSkillQuality(ctx, request)
	}
	provider, model := analyzerProviderModel(a.next())
	path, err := a.qualityPath(request.ContentHash, provider, model)
	if err != nil {
		return SkillQualityResult{}, err
	}
	cached, err := readSkillQualityCache(path)
	if err == nil && cached.Version == skillQualityCacheVersion && cached.Result.ContentHash == request.ContentHash {
		return normalizeSkillQualityResult(cached.Result, request.ContentHash), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return SkillQualityResult{}, err
	}
	result, err := a.next().LintSkillQuality(ctx, request)
	if err != nil {
		return SkillQualityResult{}, err
	}
	result = normalizeSkillQualityResult(result, request.ContentHash)
	return result, writeSkillQualityCache(path, skillQualityCacheFile{Version: skillQualityCacheVersion, Result: result})
}

func (a CachedAnalyzer) ProviderName() string {
	provider, _ := analyzerProviderModel(a.next())
	return provider
}

func (a CachedAnalyzer) ModelName() string {
	_, model := analyzerProviderModel(a.next())
	return model
}

func (a CachedAnalyzer) next() Analyzer {
	if a.Next == nil {
		return DisabledAnalyzer{}
	}
	return a.Next
}

func (g CachedDraftGenerator) GenerateMergedSkillDraft(ctx context.Context, request DraftRequest) (DraftResult, error) {
	if request.PromptVersion == "" {
		request.PromptVersion = DraftPromptVersion
	}
	if g.Dir == "" || len(request.Skills) < 2 {
		return g.next().GenerateMergedSkillDraft(ctx, request)
	}
	path, err := g.draftPath(request)
	if err != nil {
		return DraftResult{}, err
	}
	cached, err := readDraftCache(path)
	if err == nil && cached.PromptVersion == request.PromptVersion && strings.TrimSpace(cached.Markdown) != "" {
		return cached, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return DraftResult{}, err
	}
	result, err := g.next().GenerateMergedSkillDraft(ctx, request)
	if err != nil {
		return DraftResult{}, err
	}
	if result.PromptVersion == "" {
		result.PromptVersion = request.PromptVersion
	}
	return result, writeDraftCache(path, result)
}

func (g CachedDraftGenerator) next() DraftGenerator {
	if g.Next == nil {
		return disabledDraftGenerator{}
	}
	return g.Next
}

func (g CachedDraftGenerator) draftPath(request DraftRequest) (string, error) {
	key, err := draftCacheKey(request, g.next())
	if err != nil {
		return "", err
	}
	return filepath.Join(DraftCacheDir(g.Dir), key+".json"), nil
}

func (a CachedAnalyzer) summaryPath(contentHash string) string {
	return SummaryCachePath(a.Dir, contentHash)
}

func SummaryCachePath(dir, contentHash string) string {
	return filepath.Join(dir, safeFileName(contentHash)+".json")
}

func OverlapCacheDir(dir string) string {
	return filepath.Join(dir, "overlaps")
}

func QualityCacheDir(dir string) string {
	return filepath.Join(dir, "quality")
}

func DraftCacheDir(dir string) string {
	return filepath.Join(dir, "drafts")
}

func (a CachedAnalyzer) overlapPath(summaries []GeneratedSummary) (string, error) {
	key, err := overlapCacheKey(summaries)
	if err != nil {
		return "", err
	}
	return filepath.Join(OverlapCacheDir(a.Dir), key+".json"), nil
}

func (a CachedAnalyzer) qualityPath(contentHash, provider, model string) (string, error) {
	key, err := skillQualityCacheKey(contentHash, provider, model)
	if err != nil {
		return "", err
	}
	return filepath.Join(QualityCacheDir(a.Dir), key+".json"), nil
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

type overlapCacheEntry struct {
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Summary     string `json:"summary"`
}

type draftCacheEntry struct {
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
	BodyHash    string `json:"body_hash,omitempty"`
}

type draftCacheKeyInput struct {
	Version  string            `json:"version"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Entries  []draftCacheEntry `json:"entries"`
}

type overlapCacheKeyInput struct {
	Version string              `json:"version"`
	Entries []overlapCacheEntry `json:"entries"`
}

type overlapCacheFile struct {
	Version  string            `json:"version"`
	Overlaps []SemanticOverlap `json:"overlaps"`
}

type skillQualityCacheKeyInput struct {
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
}

type skillQualityCacheFile struct {
	Version string             `json:"version"`
	Result  SkillQualityResult `json:"result"`
}

func overlapCacheKey(summaries []GeneratedSummary) (string, error) {
	entries := make([]overlapCacheEntry, 0, len(summaries))
	for _, summary := range summaries {
		entries = append(entries, overlapCacheEntry{
			Name:        strings.TrimSpace(summary.Name),
			ContentHash: strings.TrimSpace(summary.ContentHash),
			Provider:    strings.TrimSpace(summary.Provider),
			Model:       strings.TrimSpace(summary.Model),
			Summary:     strings.TrimSpace(summary.Summary),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left != right {
			return left < right
		}
		if entries[i].ContentHash != entries[j].ContentHash {
			return entries[i].ContentHash < entries[j].ContentHash
		}
		return entries[i].Summary < entries[j].Summary
	})
	data, err := json.Marshal(overlapCacheKeyInput{Version: overlapCacheVersion, Entries: entries})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func skillQualityCacheKey(contentHash, provider, model string) (string, error) {
	data, err := json.Marshal(skillQualityCacheKeyInput{
		Version:     skillQualityCacheVersion,
		ContentHash: strings.TrimSpace(contentHash),
		Provider:    strings.TrimSpace(provider),
		Model:       strings.TrimSpace(model),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func draftCacheKey(request DraftRequest, generator DraftGenerator) (string, error) {
	provider, model := generatorIdentity(generator)
	entries := make([]draftCacheEntry, 0, len(request.Skills))
	for _, skill := range request.Skills {
		contentHash := strings.TrimSpace(skill.ContentHash)
		bodyHash := ""
		if contentHash == "" {
			bodyHash = draftSkillMaterialHash(skill)
		}
		entries = append(entries, draftCacheEntry{Name: strings.TrimSpace(skill.Name), ContentHash: contentHash, BodyHash: bodyHash})
	}
	sort.Slice(entries, func(i, j int) bool {
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left != right {
			return left < right
		}
		if entries[i].ContentHash != entries[j].ContentHash {
			return entries[i].ContentHash < entries[j].ContentHash
		}
		return entries[i].BodyHash < entries[j].BodyHash
	})
	data, err := json.Marshal(draftCacheKeyInput{Version: request.PromptVersion, Provider: provider, Model: model, Entries: entries})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func generatorIdentity(generator DraftGenerator) (string, string) {
	identified, ok := generator.(IdentifiedGenerator)
	if !ok {
		return "unknown", "unknown"
	}
	provider := strings.TrimSpace(identified.ProviderName())
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(identified.ModelName())
	if model == "" {
		model = "unknown"
	}
	return provider, model
}

func draftSkillMaterialHash(skill DraftSkill) string {
	data, err := json.Marshal(skill)
	if err != nil {
		return hashText(strings.Join([]string{skill.Name, skill.Description, skill.Body}, "\n"))
	}
	return hashText(string(data))
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readOverlapCache(path string) (overlapCacheFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return overlapCacheFile{}, err
	}
	var cached overlapCacheFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return overlapCacheFile{}, err
	}
	return cached, nil
}

func writeOverlapCache(path string, cached overlapCacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func readSkillQualityCache(path string) (skillQualityCacheFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return skillQualityCacheFile{}, err
	}
	var cached skillQualityCacheFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return skillQualityCacheFile{}, err
	}
	return cached, nil
}

func writeSkillQualityCache(path string, cached skillQualityCacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func readDraftCache(path string) (DraftResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DraftResult{}, err
	}
	var cached DraftResult
	if err := json.Unmarshal(data, &cached); err != nil {
		return DraftResult{}, err
	}
	return cached, nil
}

func writeDraftCache(path string, result DraftResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func normalizeSkillQualityResult(result SkillQualityResult, contentHash string) SkillQualityResult {
	if result.Issues == nil {
		result.Issues = []SkillQualityIssue{}
	}
	if result.ContentHash == "" {
		result.ContentHash = contentHash
	}
	for i := range result.Issues {
		if result.Issues[i].Provider == "" {
			result.Issues[i].Provider = result.Provider
		}
		if result.Issues[i].Model == "" {
			result.Issues[i].Model = result.Model
		}
	}
	return result
}

func analyzerProviderModel(analyzer Analyzer) (string, string) {
	identified, ok := analyzer.(ProviderModel)
	if !ok {
		return "unknown", "unknown"
	}
	provider := strings.TrimSpace(identified.ProviderName())
	model := strings.TrimSpace(identified.ModelName())
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	return provider, model
}

type disabledDraftGenerator struct{}

func (disabledDraftGenerator) GenerateMergedSkillDraft(ctx context.Context, request DraftRequest) (DraftResult, error) {
	return DraftResult{}, errors.New("LLM merged-draft generation is disabled; rerun with --with-llm and GEMINI_API_KEY or enable LLM-assisted analysis in setup")
}

func (disabledDraftGenerator) ProviderName() string { return "disabled" }
func (disabledDraftGenerator) ModelName() string    { return "disabled" }

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
