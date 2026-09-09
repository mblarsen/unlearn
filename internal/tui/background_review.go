package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
)

// BackgroundReviewer supplies incremental review events and persists only the
// dashboard's reconciled current snapshot.
type BackgroundReviewer interface {
	Run(context.Context, func(audit.ReviewEvent)) error
	Persist(inventorysnapshot.Snapshot, []audit.Diagnostic, bool) error
}

type backgroundReviewLifecycle struct {
	review BackgroundReviewer
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
	events chan audit.ReviewEvent
	active bool
}

type backgroundReviewMsg struct {
	Event  audit.ReviewEvent
	Closed bool
}

// WithBackgroundReview adds optional analysis that starts from Init, after the
// dashboard model already contains its local deterministic result.
func (m Model) WithBackgroundReview(review BackgroundReviewer) Model {
	return m.WithBackgroundReviewContext(context.Background(), review)
}

// WithBackgroundReviewContext also cancels network work when the owning command
// context is interrupted or ends unexpectedly.
func (m Model) WithBackgroundReviewContext(ctx context.Context, review BackgroundReviewer) Model {
	if review != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		m.backgroundReview = &backgroundReviewLifecycle{review: review, parent: ctx}
		m.ReviewActive = true
	}
	return m
}

func (l *backgroundReviewLifecycle) start() tea.Cmd {
	if l == nil || l.review == nil || l.active {
		return nil
	}
	parent := l.parent
	if parent == nil {
		parent = context.Background()
	}
	l.ctx, l.cancel = context.WithCancel(parent)
	l.events = make(chan audit.ReviewEvent, 1)
	l.active = true
	go func() {
		_ = l.review.Run(l.ctx, func(event audit.ReviewEvent) {
			select {
			case l.events <- event:
			case <-l.ctx.Done():
			}
		})
		close(l.events)
	}()
	return l.wait()
}

func (l *backgroundReviewLifecycle) wait() tea.Cmd {
	if l == nil || l.events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-l.events
		return backgroundReviewMsg{Event: event, Closed: !ok}
	}
}

func (l *backgroundReviewLifecycle) stop() {
	if l == nil {
		return
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.active = false
}

func (m Model) updateBackgroundReview(msg backgroundReviewMsg) (tea.Model, tea.Cmd) {
	if msg.Closed {
		m.ReviewActive = false
		return m, nil
	}
	event := msg.Event
	changed := false
	if event.Progress != nil {
		m.ReviewStep = event.Progress.Step
		m.ReviewCurrent = event.Progress.Current
		m.ReviewTotal = event.Progress.Total
		m.ReviewDetail = event.Progress.Detail
	}
	if event.Summary != nil {
		changed = m.applyReviewSummary(*event.Summary, event.SummarySkills) || changed
	}
	if event.Finding != nil {
		if finding, ok := m.reconcileReviewFinding(*event.Finding); ok {
			selection := m.captureFindingSelection()
			m.Findings = analysis.MergeLLMFindings(m.Findings, []analysis.Finding{finding})
			m.restoreFindingSelection(selection)
			changed = true
		}
	}
	if event.Diagnostic != nil {
		m.ReviewDiagnostics = appendUniqueDiagnostics(m.ReviewDiagnostics, *event.Diagnostic)
		changed = true
	}
	if changed {
		m.SkillGroups = groupedSkills(m.Skills)
	}
	if event.Done {
		m.ReviewActive = false
		m.ReviewDone = true
	}
	if changed || event.Done {
		m.persistBackgroundReview(event.Done && len(m.ReviewDiagnostics) == 0)
	}
	if event.Done {
		return m, nil
	}
	return m, m.backgroundReview.wait()
}

func (m *Model) applyReviewSummary(summary llm.GeneratedSummary, reviewedSkills []inventory.Skill) bool {
	changed := false
	for _, reviewed := range reviewedSkills {
		if !strings.EqualFold(strings.TrimSpace(reviewed.Name), strings.TrimSpace(summary.Name)) || reviewed.ContentHash != summary.ContentHash {
			continue
		}
		for i := range m.Skills {
			if inventorysnapshot.SameInstall(m.Skills[i], reviewed) && m.Skills[i].ContentHash == reviewed.ContentHash {
				m.Skills[i].LLMSummary = summary.Summary
				m.Skills[i].LLMProvider = summary.Provider
				m.Skills[i].LLMModel = summary.Model
				changed = true
				break
			}
		}
	}
	return changed
}

type findingSelection struct {
	id         string
	install    inventory.Skill
	hasInstall bool
}

func (m Model) captureFindingSelection() findingSelection {
	finding, ok := m.selectedFinding()
	if !ok {
		return findingSelection{}
	}
	selection := findingSelection{id: finding.ID}
	if len(finding.Skills) > 0 {
		selection.install = finding.Skills[m.clampedDetailCursor(finding)]
		selection.hasInstall = true
	}
	return selection
}

func (m *Model) restoreFindingSelection(selection findingSelection) {
	if selection.id == "" || m.Mode != ViewFindings {
		return
	}
	cursor := 0
	for _, section := range groupedFindings(m.Findings) {
		for _, finding := range section.Findings {
			if finding.ID != selection.id {
				cursor++
				continue
			}
			m.Cursor = cursor
			if selection.hasInstall {
				for i, skill := range finding.Skills {
					if inventorysnapshot.SameInstall(skill, selection.install) {
						m.DetailCursor = i
						return
					}
				}
			}
			m.DetailCursor = m.clampedDetailCursor(finding)
			return
		}
	}
}

func (m Model) reconcileReviewFinding(finding analysis.Finding) (analysis.Finding, bool) {
	reconciled := finding
	reconciled.Skills = make([]inventory.Skill, 0, len(finding.Skills))
	for _, reviewed := range finding.Skills {
		matched := false
		for _, current := range m.Skills {
			if inventorysnapshot.SameInstall(current, reviewed) && current.ContentHash == reviewed.ContentHash {
				reconciled.Skills = append(reconciled.Skills, current)
				matched = true
				break
			}
		}
		if !matched {
			return analysis.Finding{}, false
		}
	}
	if len(reconciled.Skills) == 0 {
		return analysis.Finding{}, false
	}
	if (reconciled.Type == analysis.FindingOverlap || reconciled.Type == analysis.FindingDuplicate || reconciled.Type == analysis.FindingConflict) && len(reconciled.Skills) < 2 {
		return analysis.Finding{}, false
	}
	return reconciled, true
}

func (m *Model) persistBackgroundReview(complete bool) {
	if m.backgroundReview == nil || m.backgroundReview.review == nil {
		return
	}
	if err := m.backgroundReview.review.Persist(m.snapshot(), m.ReviewDiagnostics, complete); err != nil {
		diagnostic := audit.Diagnostic{Code: audit.DiagnosticLLMFallback, Message: "Persist background review: " + llm.RedactDiagnostic(err.Error())}
		m.ReviewDiagnostics = appendUniqueDiagnostics(m.ReviewDiagnostics, diagnostic)
	}
}

func appendUniqueDiagnostics(values []audit.Diagnostic, addition audit.Diagnostic) []audit.Diagnostic {
	for _, value := range values {
		if value.Code == addition.Code && value.Message == addition.Message && value.Path == addition.Path {
			return values
		}
	}
	return append(values, addition)
}

func (m *Model) openReviewDiagnostics() bool {
	if len(m.ReviewDiagnostics) == 0 {
		return false
	}
	messages := make([]string, 0, len(m.ReviewDiagnostics))
	for _, diagnostic := range m.ReviewDiagnostics {
		messages = append(messages, diagnostic.Message)
	}
	m.setStatus("Background LLM review error:\n" + strings.Join(messages, "\n\n"))
	m.StatusRecovery = "Completed results were kept. Restart the dashboard to retry unfinished cached steps."
	m.FeedbackScroll = 0
	m.State = StateFeedback
	return true
}

func (m Model) reviewHeaderLabel() string {
	if len(m.ReviewDiagnostics) > 0 {
		return "LLM error · e details"
	}
	if m.ReviewActive {
		stage := "review"
		switch m.ReviewStep {
		case "llm-summary":
			stage = "summaries"
		case "llm-overlap":
			stage = "overlaps"
		case "llm-quality":
			stage = "quality"
		}
		if m.ReviewTotal > 0 {
			return fmt.Sprintf("LLM %s %d/%d", stage, m.ReviewCurrent, m.ReviewTotal)
		}
		return "LLM " + stage
	}
	if m.ReviewDone {
		return "LLM complete"
	}
	return ""
}
