package analysis

import (
	"context"
	"errors"
	"fmt"

	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

// LLMReviewEventKind identifies one incremental result from optional LLM review.
type LLMReviewEventKind string

const (
	LLMReviewProgress  LLMReviewEventKind = "progress"
	LLMReviewSummary   LLMReviewEventKind = "summary"
	LLMReviewFinding   LLMReviewEventKind = "finding"
	LLMReviewFailure   LLMReviewEventKind = "failure"
	LLMReviewCompleted LLMReviewEventKind = "completed"
)

// LLMReviewEvent is a narrow stream item. Summary and Finding carry completed
// work; failures do not invalidate earlier or independent results.
type LLMReviewEvent struct {
	Kind     LLMReviewEventKind
	Stage    string
	Progress ProgressEvent
	Summary  llm.GeneratedSummary
	Finding  Finding
	Err      error
}

// ReviewWithLLM runs optional review requests sequentially and emits completed
// work as soon as it is available. Individual provider failures are collected
// while independent requests continue; cancellation stops the sequence.
func ReviewWithLLM(ctx context.Context, skills []inventory.Skill, analyzer llm.Analyzer, emit func(LLMReviewEvent)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if analyzer == nil {
		return nil
	}
	emitEvent := func(event LLMReviewEvent) {
		if emit != nil {
			emit(event)
		}
	}
	report := func(progress ProgressEvent) {
		emitEvent(LLMReviewEvent{Kind: LLMReviewProgress, Stage: progress.Step, Progress: progress})
	}
	failures := make([]error, 0)
	fail := func(stage string, err error) {
		wrapped := fmt.Errorf("%s: %w", stage, err)
		failures = append(failures, wrapped)
		emitEvent(LLMReviewEvent{Kind: LLMReviewFailure, Stage: stage, Err: wrapped})
	}

	logicalSkills := representativeSkills(skills)
	summaries := make([]llm.GeneratedSummary, 0, len(logicalSkills))
	byName := map[string]inventory.Skill{}
	for index, skill := range logicalSkills {
		if err := ctx.Err(); err != nil {
			return err
		}
		report(ProgressEvent{Step: "llm-summary", Current: index + 1, Total: len(logicalSkills), Detail: skill.Name})
		summary, err := analyzer.Summarize(ctx, skill.Name, deterministicSummary(skill), skill.ContentHash)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			fail("summary "+skill.Name, err)
			continue
		}
		summaries = append(summaries, summary)
		byName[logicalName(skill)] = skill
		emitEvent(LLMReviewEvent{Kind: LLMReviewSummary, Stage: "llm-summary", Summary: summary})
	}
	report(ProgressEvent{Step: "llm-summary", Current: len(logicalSkills), Total: len(logicalSkills), Detail: fmt.Sprintf("%d skill summaries ready", len(summaries)), Done: true})

	if len(summaries) >= 2 {
		report(ProgressEvent{Step: "llm-overlap", Detail: "asking Gemini to group semantic overlaps"})
		overlaps, err := analyzer.FindOverlaps(ctx, summaries)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			fail("semantic overlap", err)
		} else {
			for _, overlap := range overlaps {
				group := make([]inventory.Skill, 0, len(overlap.SkillNames))
				for _, name := range overlap.SkillNames {
					if skill, ok := byName[logicalName(inventory.Skill{Name: name})]; ok {
						group = append(group, skill)
					}
				}
				if len(group) >= 2 {
					emitEvent(LLMReviewEvent{Kind: LLMReviewFinding, Stage: "llm-overlap", Finding: llmOverlapFinding(group, overlap)})
				}
			}
			report(ProgressEvent{Step: "llm-overlap", Current: len(overlaps), Detail: fmt.Sprintf("%d LLM overlap group(s)", len(overlaps)), Done: true})
		}
	}

	byLogicalName := skillsByLogicalName(skills)
	qualityCount := 0
	for index, skill := range logicalSkills {
		if err := ctx.Err(); err != nil {
			return err
		}
		report(ProgressEvent{Step: "llm-quality", Current: index + 1, Total: len(logicalSkills), Detail: skill.Name})
		result, err := analyzer.LintSkillQuality(ctx, skillQualityRequest(skill))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			fail("skill quality "+skill.Name, err)
			continue
		}
		if finding, ok := skillQualityFinding(byLogicalName[logicalName(skill)], result); ok {
			qualityCount++
			emitEvent(LLMReviewEvent{Kind: LLMReviewFinding, Stage: "llm-quality", Finding: finding})
		}
	}
	report(ProgressEvent{Step: "llm-quality", Current: len(logicalSkills), Total: len(logicalSkills), Detail: fmt.Sprintf("%d advisory finding(s)", qualityCount), Done: true})
	emitEvent(LLMReviewEvent{Kind: LLMReviewCompleted, Stage: "complete"})
	return errors.Join(failures...)
}
