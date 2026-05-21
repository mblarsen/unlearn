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

const overlapCacheVersion = "overlap-v1"

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
	return GeneratedSummary{Name: name, Summary: deterministicSummary, Provider: "disabled", Model: "disabled", ContentHash: contentHash}, nil
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

func (a CachedAnalyzer) next() Analyzer {
	if a.Next == nil {
		return DisabledAnalyzer{}
	}
	return a.Next
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

func (a CachedAnalyzer) overlapPath(summaries []GeneratedSummary) (string, error) {
	key, err := overlapCacheKey(summaries)
	if err != nil {
		return "", err
	}
	return filepath.Join(OverlapCacheDir(a.Dir), key+".json"), nil
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

type overlapCacheKeyInput struct {
	Version string              `json:"version"`
	Entries []overlapCacheEntry `json:"entries"`
}

type overlapCacheFile struct {
	Version  string            `json:"version"`
	Overlaps []SemanticOverlap `json:"overlaps"`
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
