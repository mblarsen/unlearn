package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/review"
	"github.com/mblarsen/unlearn/internal/ui"
)

func (m *Model) beginGuidedReview() {
	saved, kept := m.Actions.GuidedReviewState()
	session := review.Start(m.Findings, kept, saved)
	if err := m.Actions.SaveGuidedReviewState(session.State()); err != nil {
		m.fail(fmt.Errorf("guided review could not save its scope: %w", err))
		return
	}
	m.GuidedReview = session
	m.Mode = ViewGuidedReview
	m.Cursor = 0
	m.DetailCursor = 0
	m.dismissStatus()
}

func (m Model) updateGuidedReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.Mode = ViewFindings
		m.Cursor = 0
		m.DetailCursor = 0
	case "?":
		m.State = StateHelp
	case "k":
		item, ok := m.GuidedReview.Current()
		if !ok {
			return m, nil
		}
		if err := m.Actions.KeepSkill(item.Target); err != nil {
			m.fail(err)
			return m, nil
		}
		if err := m.recordGuidedReviewDecision(review.ActionKeep); err != nil {
			m.fail(fmt.Errorf("kept %s but review progress was not saved: %w", item.Target.Name, err))
			return m, nil
		}
		m.setStatus("kept " + item.Target.Name + " and advanced the review")
	case "l":
		if _, ok := m.GuidedReview.Current(); !ok {
			return m, nil
		}
		if err := m.recordGuidedReviewDecision(review.ActionRevisit); err != nil {
			m.fail(fmt.Errorf("revisit decision was not saved: %w", err))
			return m, nil
		}
		m.setStatus("saved for a later review")
	case "q":
		item, ok := m.GuidedReview.Current()
		if !ok {
			return m, nil
		}
		m.PendingAction = ActionQuarantine
		m.PendingSkill = item.Target
		m.PendingSkills = []inventory.Skill{item.Target}
		m.guidedReviewDecisionPending = true
		m.continuePendingWithSelectedSkills()
	}
	return m, nil
}

func (m *Model) recordGuidedReviewDecision(action review.Action) error {
	next, err := m.GuidedReview.Decide(action)
	if err != nil {
		return err
	}
	_, kept := m.Actions.GuidedReviewState()
	m.GuidedReview = review.Resume(m.Findings, kept, next.State())
	return m.Actions.SaveGuidedReviewState(m.GuidedReview.State())
}

func (m Model) renderGuidedReview(theme ui.Theme, width, height int) string {
	contentWidth := max(20, width-8)
	lines := []string{theme.PanelTitle.Render("Guided maintenance review"), ""}
	item, ok := m.GuidedReview.Current()
	if !ok {
		if m.GuidedReview.Total() == 0 {
			lines = append(lines,
				theme.Section.Render("No eligible review findings"),
				theme.Muted.Render("This review uses duplicate, conflict, and opted-in unseen findings."),
			)
		} else {
			lines = append(lines, theme.BadgeSuccess.Render("SCOPE COMPLETE"))
		}
		lines = append(lines, "")
		for _, line := range ui.Wrap(m.GuidedReview.CompletionMessage(), contentWidth) {
			lines = append(lines, theme.Muted.Render(line))
		}
		lines = append(lines, "", theme.Muted.Render("Esc returns to findings. Press v later to start a new review."))
		return renderGuidedReviewPanel(theme, lines, width, height)
	}

	position := m.GuidedReview.Reviewed() + 1
	lines = append(lines,
		theme.Badge.Render(fmt.Sprintf("DECISION %d OF %d", position, m.GuidedReview.Total())),
		"",
		findingBadge(theme, item.Finding.Type)+" "+theme.Accent.Render(ui.Truncate(item.Finding.Title, contentWidth-12)),
		"",
		theme.Section.Render("Evidence"),
	)
	for _, evidence := range item.Evidence {
		lines = appendWrappedBullet(lines, theme, evidence, contentWidth)
	}
	if item.Finding.Type == analysis.FindingUnseen {
		lines = appendWrappedBullet(lines, theme, "History coverage is opt-in. Not observed does not mean unused.", contentWidth)
	}
	lines = append(lines, "", theme.Section.Render("Exact install"))
	for _, line := range ui.Wrap(skillTarget(item.Target), contentWidth) {
		lines = append(lines, theme.Row.Render(line))
	}
	lines = append(lines, "", theme.Section.Render("Consequence"))
	for _, line := range ui.Wrap(item.Consequence, contentWidth) {
		lines = append(lines, theme.Row.Render(line))
	}
	lines = append(lines, "", theme.Muted.Render("k keep logical skill · q quarantine exact install · l revisit later"))
	return renderGuidedReviewPanel(theme, lines, width, height)
}

func renderGuidedReviewPanel(theme ui.Theme, lines []string, width, height int) string {
	panelWidth := max(1, width-2)
	panelHeight := max(1, height-2)
	content := strings.Join(ui.PadLines(ui.FitLines(lines, panelHeight), panelHeight), "\n")
	return theme.Panel.Width(panelWidth).Height(panelHeight).Render(content)
}

func appendWrappedBullet(lines []string, theme ui.Theme, value string, width int) []string {
	wrapped := ui.Wrap(value, max(1, width-2))
	for i, line := range wrapped {
		prefix := "  "
		if i == 0 {
			prefix = "• "
		}
		lines = append(lines, theme.Row.Render(prefix+line))
	}
	return lines
}
