package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestPrepareDashboardReturnsDeterministicResultBeforeBlockedLLMReview(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "same body")
	writeSkill(t, root, "beta", "same body")
	cfg := trustedConfig(root)
	cfg.LLMAssisted = true
	analyzer := &blockingAuditAnalyzer{started: make(chan struct{})}

	preparedAt := time.Now()
	result, review, err := PrepareDashboard(context.Background(), Policy{
		Config: cfg, Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh, LLMAnalyzer: analyzer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if review == nil || analyzer.callCount() != 0 {
		t.Fatalf("review=%v calls=%d", review, analyzer.callCount())
	}
	if time.Since(preparedAt) > time.Second {
		t.Fatal("local dashboard preparation unexpectedly waited")
	}
	if len(result.Skills) != 2 {
		t.Fatalf("deterministic result=%#v", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- review.Run(ctx, nil) }()
	select {
	case <-analyzer.started:
	case <-time.After(time.Second):
		t.Fatal("background review did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestPrepareDashboardBackgroundReviewOwnsItsBaseSnapshot(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "body")
	cfg := trustedConfig(root)
	cfg.LLMAssisted = true
	result, review, err := PrepareDashboard(context.Background(), Policy{
		Config: cfg, Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh, LLMAnalyzer: &blockingAuditAnalyzer{started: make(chan struct{})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review == nil || len(result.Skills) != 1 {
		t.Fatalf("result=%#v review=%v", result, review)
	}
	result.Skills[0].LLMSummary = "dashboard mutation"
	if review.base.Skills[0].LLMSummary != "" {
		t.Fatal("background review base shares the dashboard skill backing array")
	}
}

func TestPrepareDashboardDoesNotStartLLMWhenPolicyIsDisabled(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "body")
	analyzer := &blockingAuditAnalyzer{started: make(chan struct{})}
	result, review, err := PrepareDashboard(context.Background(), Policy{
		Config: trustedConfig(root), Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh, LLMAnalyzer: analyzer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if review != nil || analyzer.callCount() != 0 || len(result.Skills) != 1 {
		t.Fatalf("result=%#v review=%v calls=%d", result, review, analyzer.callCount())
	}
}

func TestBackgroundReviewPersistsOnlyReconciledCurrentSnapshotAcrossRestart(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "body")
	writeSkill(t, root, "beta", "body")
	paths := testPaths(t)
	cfg := trustedConfig(root)
	cfg.LLMAssisted = true
	policy := Policy{Config: cfg, Paths: paths, Roots: []string{root}, SnapshotCache: SnapshotCachePrefer, LLMAnalyzer: &partialAuditAnalyzer{}}
	result, review, err := PrepareDashboard(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	var beta = result.Skills[1]
	if result.Skills[0].Name == "beta" {
		beta = result.Skills[0]
	}
	current := inventorysnapshot.Snapshot{Skills: []inventory.Skill{beta}}
	if err := review.Persist(current, nil, false); err != nil {
		t.Fatal(err)
	}

	db, err := state.OpenIndex(paths.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _, err := state.LoadInventoryCache(db)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Name != "beta" {
		t.Fatalf("persisted=%#v", persisted)
	}

	restarted, resumed, err := PrepareDashboard(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.Skills) != 1 || restarted.Skills[0].Name != "beta" || resumed == nil {
		t.Fatalf("restart result=%#v review=%v", restarted, resumed)
	}
}

func TestBackgroundReviewRedactsFailuresAndKeepsLaterFindings(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "body")
	writeSkill(t, root, "beta", "body")
	cfg := trustedConfig(root)
	cfg.LLMAssisted = true
	analyzer := &partialAuditAnalyzer{}
	_, review, err := PrepareDashboard(context.Background(), Policy{
		Config: cfg, Paths: testPaths(t), Roots: []string{root}, LLMAnalyzer: analyzer,
	})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics, findings int
	err = review.Run(context.Background(), func(event ReviewEvent) {
		if event.Diagnostic != nil {
			diagnostics++
			if got := event.Diagnostic.Message; got == "" || containsSecret(got) {
				t.Fatalf("unredacted diagnostic=%q", got)
			}
		}
		if event.Finding != nil {
			findings++
		}
	})
	if err == nil || diagnostics == 0 || findings == 0 {
		t.Fatalf("error=%v diagnostics=%d findings=%d", err, diagnostics, findings)
	}
}

func containsSecret(value string) bool {
	return len(value) > 0 && (value == "secret" || stringContains(value, "api_key=secret"))
}

func stringContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

type blockingAuditAnalyzer struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
}

func (a *blockingAuditAnalyzer) callCount() int { a.mu.Lock(); defer a.mu.Unlock(); return a.calls }
func (a *blockingAuditAnalyzer) Summarize(ctx context.Context, name, summary, hash string) (llm.GeneratedSummary, error) {
	a.mu.Lock()
	a.calls++
	if a.calls == 1 {
		close(a.started)
	}
	a.mu.Unlock()
	<-ctx.Done()
	return llm.GeneratedSummary{}, ctx.Err()
}
func (*blockingAuditAnalyzer) FindOverlaps(context.Context, []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	return nil, nil
}
func (*blockingAuditAnalyzer) LintSkillQuality(context.Context, llm.SkillQualityRequest) (llm.SkillQualityResult, error) {
	return llm.SkillQualityResult{}, nil
}
func (*blockingAuditAnalyzer) ProviderName() string { return "blocked" }
func (*blockingAuditAnalyzer) ModelName() string    { return "test" }

type partialAuditAnalyzer struct{}

func (*partialAuditAnalyzer) Summarize(_ context.Context, name, summary, hash string) (llm.GeneratedSummary, error) {
	if name == "alpha" {
		return llm.GeneratedSummary{}, errors.New("https://example.test?api_key=secret")
	}
	return llm.GeneratedSummary{Name: name, Summary: summary, Provider: "test", Model: "fake", ContentHash: hash}, nil
}
func (*partialAuditAnalyzer) FindOverlaps(context.Context, []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	return nil, nil
}
func (*partialAuditAnalyzer) LintSkillQuality(_ context.Context, request llm.SkillQualityRequest) (llm.SkillQualityResult, error) {
	if request.Name == "beta" {
		return llm.SkillQualityResult{Issues: []llm.SkillQualityIssue{{IssueType: "vague", Reason: "too vague", Recommendation: "be specific"}}, Provider: "test", Model: "fake", ContentHash: request.ContentHash}, nil
	}
	return llm.SkillQualityResult{Provider: "test", Model: "fake", ContentHash: request.ContentHash}, nil
}
func (*partialAuditAnalyzer) ProviderName() string { return "partial" }
func (*partialAuditAnalyzer) ModelName() string    { return "test" }
