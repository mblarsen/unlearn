package analysis

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

func TestReviewWithLLMStreamsCompletedWorkAndContinuesAfterIndividualFailures(t *testing.T) {
	t.Parallel()
	skills := []inventory.Skill{
		{Name: "alpha", ContentHash: "a"},
		{Name: "beta", ContentHash: "b"},
		{Name: "gamma", ContentHash: "c"},
	}
	analyzer := &scriptedReviewAnalyzer{
		summaryErrors: map[string]error{"alpha": errors.New("summary alpha failed")},
		qualityErrors: map[string]error{"a": errors.New("quality alpha failed")},
	}
	var events []LLMReviewEvent
	err := ReviewWithLLM(context.Background(), skills, analyzer, func(event LLMReviewEvent) {
		events = append(events, event)
	})
	if err == nil {
		t.Fatal("expected joined review error")
	}
	if got, want := analyzer.calls, []string{
		"summary:alpha", "summary:beta", "summary:gamma",
		"overlaps:beta,gamma",
		"quality:alpha", "quality:beta", "quality:gamma",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls=%#v want=%#v", got, want)
	}
	var summaries, findings, failures, completed int
	for _, event := range events {
		switch event.Kind {
		case LLMReviewSummary:
			summaries++
		case LLMReviewFinding:
			findings++
		case LLMReviewFailure:
			failures++
		case LLMReviewCompleted:
			completed++
		}
	}
	if summaries != 2 || findings != 2 || failures != 2 || completed != 1 {
		t.Fatalf("summaries=%d findings=%d failures=%d completed=%d events=%#v", summaries, findings, failures, completed, events)
	}
}

func TestReviewWithLLMStopsPromptlyOnCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	analyzer := &scriptedReviewAnalyzer{summaryStarted: started, blockSummary: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ReviewWithLLM(ctx, []inventory.Skill{{Name: "alpha", ContentHash: "a"}}, analyzer, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("review did not reach analyzer")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("review did not stop after cancellation")
	}
}

type scriptedReviewAnalyzer struct {
	mu             sync.Mutex
	calls          []string
	summaryErrors  map[string]error
	qualityErrors  map[string]error
	summaryStarted chan struct{}
	blockSummary   bool
}

func (a *scriptedReviewAnalyzer) record(call string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, call)
}

func (a *scriptedReviewAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (llm.GeneratedSummary, error) {
	a.record("summary:" + name)
	if a.summaryStarted != nil {
		select {
		case <-a.summaryStarted:
		default:
			close(a.summaryStarted)
		}
	}
	if a.blockSummary {
		<-ctx.Done()
		return llm.GeneratedSummary{}, ctx.Err()
	}
	if err := a.summaryErrors[name]; err != nil {
		return llm.GeneratedSummary{}, err
	}
	return llm.GeneratedSummary{Name: name, Summary: "generated " + name, Provider: "test", Model: "fake", ContentHash: contentHash}, nil
}

func (a *scriptedReviewAnalyzer) FindOverlaps(_ context.Context, summaries []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	names := summaries[0].Name
	for _, summary := range summaries[1:] {
		names += "," + summary.Name
	}
	a.record("overlaps:" + names)
	return []llm.SemanticOverlap{{SkillNames: []string{"beta", "gamma"}, Reason: "related", Provider: "test", Model: "fake"}}, nil
}

func (a *scriptedReviewAnalyzer) LintSkillQuality(_ context.Context, request llm.SkillQualityRequest) (llm.SkillQualityResult, error) {
	a.record("quality:" + request.Name)
	if err := a.qualityErrors[request.ContentHash]; err != nil {
		return llm.SkillQualityResult{}, err
	}
	result := llm.SkillQualityResult{Provider: "test", Model: "fake", ContentHash: request.ContentHash}
	if request.Name == "beta" {
		result.Issues = []llm.SkillQualityIssue{{IssueType: "vague", Reason: "too vague", Recommendation: "be specific"}}
	}
	return result, nil
}
