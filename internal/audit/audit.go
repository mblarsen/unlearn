// Package audit owns the read-only skill audit pipeline, including trusted-root
// discovery, opt-in evidence coverage, analysis, and snapshot caching.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
	setupflow "github.com/mblarsen/unlearn/internal/setup"
	"github.com/mblarsen/unlearn/internal/state"
	"github.com/mblarsen/unlearn/internal/usage"
)

// EvidenceCoverage states whether absence of invocation evidence is meaningful.
type EvidenceCoverage string

const (
	EvidenceUnknown    EvidenceCoverage = "unknown"
	EvidenceIncomplete EvidenceCoverage = "incomplete"
	EvidenceComplete   EvidenceCoverage = "complete"
)

// SnapshotCachePolicy controls the local dashboard snapshot without exposing
// its SQLite representation to callers.
type SnapshotCachePolicy string

const (
	SnapshotCacheBypass  SnapshotCachePolicy = "bypass"
	SnapshotCachePrefer  SnapshotCachePolicy = "prefer"
	SnapshotCacheRefresh SnapshotCachePolicy = "refresh"
)

type DiagnosticCode string

const (
	DiagnosticMissingHistorySource DiagnosticCode = "missing-history-source"
	DiagnosticLLMUnavailable       DiagnosticCode = "llm-unavailable"
	DiagnosticLLMFallback          DiagnosticCode = "llm-fallback"
)

type Diagnostic struct {
	Code    DiagnosticCode
	Message string
	Path    string
}

type Progress struct {
	Step    string
	Current int
	Total   int
	Detail  string
	Done    bool
}

// Policy contains user-selected scan and privacy policy. History paths are
// read only when explicitly supplied here or enabled in Config.
type Policy struct {
	Config config.Config
	Paths  state.Paths

	Roots          []string
	ActiveAgents   []string
	InactiveAgents []string

	HistoryJSONL    []string
	HistorySQLite   []string
	HistoryCacheTTL time.Duration
	RescanSources   bool

	SnapshotCache SnapshotCachePolicy
	LLMAnalyzer   llm.Analyzer

	Progress        func(Progress)
	HistoryProgress func(history.ScanProgress)
}

// Result is the complete observable audit outcome. Callers render diagnostics
// but do not decide whether evidence is sufficient for unseen findings.
type Result struct {
	Skills            []inventory.Skill
	Findings          []analysis.Finding
	SkippedRoots      []string
	EvidenceCoverage  EvidenceCoverage
	Diagnostics       []Diagnostic
	FromSnapshotCache bool
}

const auditCacheMetadataKey = "audit-metadata-v1"

type cacheMetadata struct {
	EvidenceCoverage EvidenceCoverage `json:"evidence_coverage"`
	SkippedRoots     []string         `json:"skipped_roots,omitempty"`
	Diagnostics      []Diagnostic     `json:"diagnostics,omitempty"`
	Provenance       cacheProvenance  `json:"provenance"`
}

type cacheProvenance struct {
	Roots          []string `json:"roots,omitempty"`
	TrustedRoots   []string `json:"trusted_roots,omitempty"`
	ActiveAgents   []string `json:"active_agents,omitempty"`
	InactiveAgents []string `json:"inactive_agents,omitempty"`
	HistoryScan    bool     `json:"history_scan"`
	HistoryJSONL   []string `json:"history_jsonl,omitempty"`
	HistorySQLite  []string `json:"history_sqlite,omitempty"`
	ExplicitJSONL  []string `json:"explicit_jsonl,omitempty"`
	ExplicitSQLite []string `json:"explicit_sqlite,omitempty"`
	LLMAssisted    bool     `json:"llm_assisted"`
	LLMProvider    string   `json:"llm_provider,omitempty"`
	LLMModel       string   `json:"llm_model,omitempty"`
}

type selection struct {
	trustedRoots   []string
	skippedRoots   []string
	activeAgents   []string
	inactiveAgents []string
	provenance     cacheProvenance
}

// Run executes one audit according to policy.
func Run(ctx context.Context, policy Policy) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := policy.Paths.Ensure(); err != nil {
		return Result{}, err
	}
	selection := resolveSelection(policy)
	if policy.SnapshotCache == SnapshotCachePrefer && !policy.RescanSources {
		if result, ok, err := loadSnapshotCache(ctx, policy, selection.provenance); err != nil {
			return Result{}, err
		} else if ok {
			return result, nil
		}
	}

	report(policy.Progress, Progress{Step: "scan-roots", Detail: fmt.Sprintf("%d trusted root(s), %d skipped", len(selection.trustedRoots), len(selection.skippedRoots))})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	scan, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: selection.trustedRoots, RootOwnerships: inventory.RootOwnershipForAgents(selection.activeAgents, selection.inactiveAgents)})
	if err != nil {
		return Result{}, err
	}
	report(policy.Progress, Progress{Step: "scan-roots", Detail: fmt.Sprintf("%d skill install(s)", len(scan.Skills)), Done: true})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	usageResult, err := usage.Load(usage.Options{
		Config: policy.Config, Paths: policy.Paths, Skills: scan.Skills, TrustedRoots: selection.trustedRoots,
		HistoryJSONL: policy.HistoryJSONL, HistorySQLite: policy.HistorySQLite,
		HistoryCacheTTL: policy.HistoryCacheTTL, RescanSources: policy.RescanSources,
		Context: ctx, HistoryProgress: policy.HistoryProgress,
		Progress: func(event usage.Progress) {
			report(policy.Progress, Progress{Step: event.Step, Current: event.Current, Total: event.Total, Detail: event.Detail, Done: event.Done})
		},
	})
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	coverage := evidenceCoverage(usageResult)
	skills := usageResult.Skills
	if skills == nil {
		skills = scan.Skills
	}
	result := Result{Skills: skills, SkippedRoots: selection.skippedRoots, EvidenceCoverage: coverage}
	for _, path := range usageResult.MissingSources {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: DiagnosticMissingHistorySource, Path: path,
			Message: fmt.Sprintf("History source is no longer available; skipped: %s", path),
		})
	}

	report(policy.Progress, Progress{Step: "analysis", Detail: "duplicates, conflicts, keywords, safety findings"})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	analysisOptions := analysis.Options{UsageEvidence: eligibleUsageEvidence(coverage, usageResult.Evidence), Progress: func(event analysis.ProgressEvent) {
		report(policy.Progress, Progress{Step: event.Step, Current: event.Current, Total: event.Total, Detail: event.Detail, Done: event.Done})
	}}
	var recorder *recordingAnalyzer
	if policy.Config.LLMAssisted {
		if policy.LLMAnalyzer == nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: DiagnosticLLMUnavailable, Message: "LLM analysis requested, but GEMINI_API_KEY/GOOGLE_API_KEY is not set; using deterministic analysis."})
		} else {
			recorder = newRecordingAnalyzer(llm.NewCachedAnalyzer(policy.Paths.LLMCacheDir, policy.LLMAnalyzer))
			analysisOptions.LLMAnalyzer = recorder
		}
	}
	findings, analysisErr := analysis.AnalyzeWithLLM(ctx, skills, analysisOptions)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if analysisErr != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: DiagnosticLLMFallback, Message: fmt.Sprintf("LLM analysis did not complete; continuing with available findings. Details: %v", analysisErr)})
		if len(findings) == 0 {
			findings = analysis.Analyze(skills, analysis.Options{UsageEvidence: eligibleUsageEvidence(coverage, usageResult.Evidence)})
		}
	}
	if recorder != nil {
		result.Skills = attachLLMSummaries(skills, recorder.Summaries())
	}
	result.Findings = findings
	report(policy.Progress, Progress{Step: "analysis", Detail: fmt.Sprintf("%d finding(s)", len(findings)), Done: true})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if policy.SnapshotCache == SnapshotCachePrefer || policy.SnapshotCache == SnapshotCacheRefresh {
		if err := saveSnapshotCache(ctx, policy.Paths.IndexPath, result, selection.provenance); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func resolveSelection(policy Policy) selection {
	activeAgents, inactiveAgents := setupflow.SelectAgentIDs(policy.ActiveAgents, policy.InactiveAgents, policy.Config, inventory.AgentStatuses())
	roots := append([]string(nil), policy.Roots...)
	if len(roots) == 0 {
		roots = inventory.RootsForAgents(append(activeAgents, inactiveAgents...))
		if len(roots) == 0 {
			roots = inventory.KnownGlobalRoots()
		}
	}
	selected := selection{activeAgents: activeAgents, inactiveAgents: inactiveAgents}
	for _, root := range roots {
		if policy.Config.IsTrusted(root) {
			selected.trustedRoots = append(selected.trustedRoots, root)
		} else {
			selected.skippedRoots = append(selected.skippedRoots, root)
		}
	}
	provider, model := analyzerIdentity(policy)
	selected.provenance = cacheProvenance{
		Roots: roots, TrustedRoots: selected.trustedRoots, ActiveAgents: activeAgents, InactiveAgents: inactiveAgents,
		HistoryScan: policy.Config.HistoryScan, HistoryJSONL: policy.Config.HistoryJSONL, HistorySQLite: policy.Config.HistorySQLite,
		ExplicitJSONL: policy.HistoryJSONL, ExplicitSQLite: policy.HistorySQLite,
		LLMAssisted: policy.Config.LLMAssisted, LLMProvider: provider, LLMModel: model,
	}
	return selected
}

func analyzerIdentity(policy Policy) (string, string) {
	if !policy.Config.LLMAssisted {
		return "", ""
	}
	if policy.LLMAnalyzer == nil {
		return "unavailable", "unavailable"
	}
	if identified, ok := policy.LLMAnalyzer.(llm.ProviderModel); ok {
		return identified.ProviderName(), identified.ModelName()
	}
	return fmt.Sprintf("%T", policy.LLMAnalyzer), "unknown"
}

func evidenceCoverage(result usage.Result) EvidenceCoverage {
	if len(result.MissingSources) > 0 {
		return EvidenceIncomplete
	}
	if result.Evidence == nil {
		return EvidenceUnknown
	}
	return EvidenceComplete
}

func eligibleUsageEvidence(coverage EvidenceCoverage, evidence analysis.UsageEvidence) analysis.UsageEvidence {
	if coverage != EvidenceComplete {
		return nil
	}
	return evidence
}

func loadSnapshotCache(ctx context.Context, policy Policy, provenance cacheProvenance) (Result, bool, error) {
	db, err := state.OpenIndex(policy.Paths.IndexPath)
	if err != nil {
		return Result{}, false, err
	}
	defer db.Close()
	report(policy.Progress, Progress{Step: "load-cache", Detail: "local dashboard index"})
	if err := ctx.Err(); err != nil {
		return Result{}, false, err
	}
	metadata, err := loadCacheMetadata(db)
	if err != nil {
		if err == sql.ErrNoRows {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	if !reflect.DeepEqual(metadata.Provenance, provenance) {
		return Result{}, false, nil
	}
	skills, findings, err := state.LoadInventoryCache(db)
	if err != nil {
		if err == sql.ErrNoRows {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	if len(skills) == 0 && len(findings) == 0 {
		return Result{}, false, nil
	}
	skills, findings, missing := state.ReconcileMissingPaths(skills, findings)
	if len(missing) > 0 {
		if err := state.ReplaceIndex(db, skills, findings); err != nil {
			return Result{}, false, err
		}
		if err := saveCacheMetadata(db, metadata); err != nil {
			return Result{}, false, err
		}
	}
	detail := fmt.Sprintf("%d skills, %d findings", len(skills), len(findings))
	if len(missing) > 0 {
		detail += fmt.Sprintf("; removed %d stale installs", len(missing))
	}
	report(policy.Progress, Progress{Step: "load-cache", Detail: detail, Done: true})
	if err := ctx.Err(); err != nil {
		return Result{}, false, err
	}
	return Result{Skills: skills, Findings: findings, SkippedRoots: metadata.SkippedRoots, EvidenceCoverage: metadata.EvidenceCoverage, Diagnostics: metadata.Diagnostics, FromSnapshotCache: true}, true, nil
}

func saveSnapshotCache(ctx context.Context, indexPath string, result Result, provenance cacheProvenance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := state.OpenIndex(indexPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := state.ReplaceIndex(db, result.Skills, result.Findings); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return saveCacheMetadata(db, cacheMetadata{EvidenceCoverage: result.EvidenceCoverage, SkippedRoots: result.SkippedRoots, Diagnostics: result.Diagnostics, Provenance: provenance})
}

func loadCacheMetadata(db *sql.DB) (cacheMetadata, error) {
	var raw string
	if err := db.QueryRow(`SELECT payload FROM inventory_cache WHERE key = ?`, auditCacheMetadataKey).Scan(&raw); err != nil {
		return cacheMetadata{}, err
	}
	var metadata cacheMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return cacheMetadata{}, err
	}
	return metadata, nil
}

func saveCacheMetadata(db *sql.DB, metadata cacheMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO inventory_cache(key, payload, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`, auditCacheMetadataKey, string(data), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func report(progress func(Progress), event Progress) {
	if progress != nil {
		progress(event)
	}
}

type recordingAnalyzer struct {
	next      llm.Analyzer
	summaries map[string]llm.GeneratedSummary
}

func newRecordingAnalyzer(next llm.Analyzer) *recordingAnalyzer {
	return &recordingAnalyzer{next: next, summaries: map[string]llm.GeneratedSummary{}}
}

func (a *recordingAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (llm.GeneratedSummary, error) {
	summary, err := a.next.Summarize(ctx, name, deterministicSummary, contentHash)
	if err == nil && strings.TrimSpace(contentHash) != "" {
		a.summaries[contentHash] = summary
	}
	return summary, err
}

func (a *recordingAnalyzer) FindOverlaps(ctx context.Context, summaries []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	return a.next.FindOverlaps(ctx, summaries)
}

func (a *recordingAnalyzer) LintSkillQuality(ctx context.Context, request llm.SkillQualityRequest) (llm.SkillQualityResult, error) {
	return a.next.LintSkillQuality(ctx, request)
}

func (a *recordingAnalyzer) ProviderName() string {
	if identified, ok := a.next.(llm.ProviderModel); ok {
		return identified.ProviderName()
	}
	return "unknown"
}

func (a *recordingAnalyzer) ModelName() string {
	if identified, ok := a.next.(llm.ProviderModel); ok {
		return identified.ModelName()
	}
	return "unknown"
}

func (a *recordingAnalyzer) Summaries() map[string]llm.GeneratedSummary { return a.summaries }

func attachLLMSummaries(skills []inventory.Skill, summaries map[string]llm.GeneratedSummary) []inventory.Skill {
	if len(summaries) == 0 {
		return skills
	}
	enriched := append([]inventory.Skill(nil), skills...)
	for i := range enriched {
		summary, ok := summaries[enriched[i].ContentHash]
		if !ok || strings.TrimSpace(summary.Summary) == "" || disabledSummary(summary) {
			continue
		}
		enriched[i].LLMSummary = summary.Summary
		enriched[i].LLMProvider = summary.Provider
		enriched[i].LLMModel = summary.Model
	}
	return enriched
}

func disabledSummary(summary llm.GeneratedSummary) bool {
	return strings.EqualFold(strings.TrimSpace(summary.Provider), "disabled") && strings.EqualFold(strings.TrimSpace(summary.Model), "disabled")
}
