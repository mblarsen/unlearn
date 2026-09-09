package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
)

// ReviewEvent is one incremental background-review update for the dashboard.
type ReviewEvent struct {
	Progress      *Progress
	Summary       *llm.GeneratedSummary
	SummarySkills []inventory.Skill
	Finding       *analysis.Finding
	Diagnostic    *Diagnostic
	Done          bool
}

// BackgroundReview owns the optional cached LLM pass and its cache provenance.
// Callers apply events to their current snapshot before asking Persist to save it.
type BackgroundReview struct {
	base       Result
	analyzer   llm.Analyzer
	indexPath  string
	provenance cacheProvenance
	cache      SnapshotCachePolicy
}

// PrepareDashboard completes local inventory, evidence, and deterministic
// analysis without making an LLM request. A non-nil review can run after the
// dashboard starts.
func PrepareDashboard(ctx context.Context, policy Policy) (Result, *BackgroundReview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, nil, err
	}
	if err := policy.Paths.Ensure(); err != nil {
		return Result{}, nil, err
	}
	selected := resolveSelection(policy)
	if policy.SnapshotCache == SnapshotCachePrefer && !policy.RescanSources {
		cached, ok, complete, err := loadSnapshotCache(ctx, policy, selected.provenance)
		if err != nil {
			return Result{}, nil, err
		}
		if ok {
			if complete {
				return cached, nil, nil
			}
			cached.Diagnostics = withoutDiagnostic(cached.Diagnostics, DiagnosticLLMFallback)
			return prepareBackgroundReview(ctx, policy, selected.provenance, cached)
		}
	}

	localPolicy := policy
	localPolicy.Config.LLMAssisted = false
	localPolicy.LLMAnalyzer = nil
	localPolicy.SnapshotCache = SnapshotCacheBypass
	result, err := Run(ctx, localPolicy)
	if err != nil {
		return Result{}, nil, err
	}
	return prepareBackgroundReview(ctx, policy, selected.provenance, result)
}

func prepareBackgroundReview(ctx context.Context, policy Policy, provenance cacheProvenance, result Result) (Result, *BackgroundReview, error) {
	if !policy.Config.LLMAssisted {
		if policy.SnapshotCache != SnapshotCacheBypass {
			if err := saveSnapshotCache(ctx, policy.Paths.IndexPath, result, provenance, true); err != nil {
				return Result{}, nil, err
			}
		}
		return result, nil, nil
	}
	if policy.LLMAnalyzer == nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: DiagnosticLLMUnavailable, Message: "LLM analysis requested, but GEMINI_API_KEY/GOOGLE_API_KEY is not set; using deterministic analysis."})
		if policy.SnapshotCache != SnapshotCacheBypass {
			if err := saveSnapshotCache(ctx, policy.Paths.IndexPath, result, provenance, true); err != nil {
				return Result{}, nil, err
			}
		}
		return result, nil, nil
	}
	owned := inventorysnapshot.Snapshot{Skills: result.Skills, Findings: result.Findings}.Clone()
	base := result
	base.Skills = owned.Skills
	base.Findings = owned.Findings
	base.Diagnostics = append([]Diagnostic(nil), result.Diagnostics...)
	review := &BackgroundReview{
		base: base, analyzer: llm.NewCachedAnalyzer(policy.Paths.LLMCacheDir, policy.LLMAnalyzer),
		indexPath: policy.Paths.IndexPath, provenance: provenance, cache: policy.SnapshotCache,
	}
	return result, review, nil
}

// Run performs the sequential optional review and emits redacted typed events.
func (r *BackgroundReview) Run(ctx context.Context, emit func(ReviewEvent)) error {
	if r == nil {
		return nil
	}
	return analysis.ReviewWithLLM(ctx, r.base.Skills, r.analyzer, func(event analysis.LLMReviewEvent) {
		if emit == nil {
			return
		}
		switch event.Kind {
		case analysis.LLMReviewProgress:
			progress := Progress{Step: event.Progress.Step, Current: event.Progress.Current, Total: event.Progress.Total, Detail: event.Progress.Detail, Done: event.Progress.Done}
			emit(ReviewEvent{Progress: &progress})
		case analysis.LLMReviewSummary:
			summary := event.Summary
			emit(ReviewEvent{Summary: &summary, SummarySkills: r.summarySkills(summary)})
		case analysis.LLMReviewFinding:
			finding := event.Finding
			emit(ReviewEvent{Finding: &finding})
		case analysis.LLMReviewFailure:
			diagnostic := Diagnostic{Code: DiagnosticLLMFallback, Message: fmt.Sprintf("LLM review step failed; continuing. Details: %s", llm.RedactDiagnostic(event.Err.Error()))}
			emit(ReviewEvent{Diagnostic: &diagnostic})
		case analysis.LLMReviewCompleted:
			emit(ReviewEvent{Done: true})
		}
	})
}

// Persist stores the caller's reconciled current snapshot. Incomplete reviews
// remain resumable on the next dashboard start.
func (r *BackgroundReview) summarySkills(summary llm.GeneratedSummary) []inventory.Skill {
	var matched []inventory.Skill
	for _, skill := range r.base.Skills {
		if strings.EqualFold(strings.TrimSpace(skill.Name), strings.TrimSpace(summary.Name)) && skill.ContentHash == summary.ContentHash {
			matched = append(matched, skill)
		}
	}
	return matched
}

func (r *BackgroundReview) Persist(snapshot inventorysnapshot.Snapshot, diagnostics []Diagnostic, complete bool) error {
	if r == nil || r.cache == SnapshotCacheBypass {
		return nil
	}
	result := r.base
	result.Skills = snapshot.Skills
	result.Findings = snapshot.Findings
	result.Diagnostics = append(withoutDiagnostic(result.Diagnostics, DiagnosticLLMFallback), diagnostics...)
	return saveSnapshotCache(context.Background(), r.indexPath, result, r.provenance, complete)
}

func withoutDiagnostic(diagnostics []Diagnostic, code DiagnosticCode) []Diagnostic {
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != code {
			out = append(out, diagnostic)
		}
	}
	return out
}
