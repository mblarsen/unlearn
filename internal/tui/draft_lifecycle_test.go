package tui

import (
	"context"
	"testing"
	"time"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

type recordingDraftGenerator struct {
	ctx     context.Context
	request llm.DraftRequest
	result  llm.DraftResult
}

func (g *recordingDraftGenerator) GenerateMergedSkillDraft(ctx context.Context, request llm.DraftRequest) (llm.DraftResult, error) {
	g.ctx = ctx
	g.request = request
	return g.result, nil
}

type controlledDraftService struct {
	NoopActionService
	started chan controlledDraftCall
	release map[string]chan struct{}
}

type controlledDraftCall struct {
	name string
	ctx  context.Context
}

func (s *controlledDraftService) DraftMerge(ctx context.Context, skills []inventory.Skill) (llm.DraftResult, error) {
	name := skills[0].Name
	s.started <- controlledDraftCall{name: name, ctx: ctx}
	<-s.release[name]
	return llm.DraftResult{Markdown: name, Provider: "controlled", Model: "test"}, nil
}

func TestConfigActionServiceUsesInjectedContextAwareDraftGenerator(t *testing.T) {
	generator := &recordingDraftGenerator{result: llm.DraftResult{Markdown: "preview"}}
	service := &ConfigActionService{Config: config.Config{LLMAssisted: true}, DraftGenerator: generator}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := service.DraftMerge(ctx, []inventory.Skill{
		{Name: "alpha", ContentHash: "hash-a", Body: "private alpha body"},
		{Name: "beta", ContentHash: "hash-b", Body: "private beta body"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Markdown != "preview" || generator.ctx != ctx {
		t.Fatalf("result=%#v context was not passed through", result)
	}
	if len(generator.request.Skills) != 2 || generator.request.Skills[0].ContentHash != "hash-a" || generator.request.PromptVersion != llm.DraftPromptVersion {
		t.Fatalf("request=%#v", generator.request)
	}
}

func TestDraftLifecycleCancelsAndRejectsStaleOperationResults(t *testing.T) {
	service := &controlledDraftService{
		started: make(chan controlledDraftCall, 2),
		release: map[string]chan struct{}{
			"A": make(chan struct{}),
			"B": make(chan struct{}),
		},
	}
	var lifecycle draftLifecycle

	cmdA := lifecycle.start(service, []inventory.Skill{{Name: "A"}, {Name: "alpha"}})
	resultA := make(chan draftMergeResultMsg, 1)
	go func() { resultA <- cmdA().(draftMergeResultMsg) }()
	callA := <-service.started
	if callA.name != "A" {
		t.Fatalf("first operation = %q, want A", callA.name)
	}

	lifecycle.cancel()
	select {
	case <-callA.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("operation A context was not cancelled")
	}

	cmdB := lifecycle.start(service, []inventory.Skill{{Name: "B"}, {Name: "beta"}})
	resultB := make(chan draftMergeResultMsg, 1)
	go func() { resultB <- cmdB().(draftMergeResultMsg) }()
	callB := <-service.started
	if callB.name != "B" {
		t.Fatalf("second operation = %q, want B", callB.name)
	}

	close(service.release["A"])
	if lifecycle.accept(<-resultA) {
		t.Fatal("stale operation A result was accepted while B was active")
	}

	close(service.release["B"])
	if !lifecycle.accept(<-resultB) {
		t.Fatal("active operation B result was rejected")
	}
}
