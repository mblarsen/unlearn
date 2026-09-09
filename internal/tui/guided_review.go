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
	m.GuidedReviewScroll = 0
	m.Mode = ViewGuidedReview
	m.Cursor = 0
	m.DetailCursor = 0
	m.dismissStatus()
}

func (m Model) updateGuidedReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if m.Status != "" {
			m.FeedbackScroll = 0
			m.State = StateFeedback
		}
	case "x":
		if m.Status != "" {
			m.dismissStatus()
		}
	case "j", "down":
		maxScroll, _ := m.guidedReviewScrollMetrics()
		m.GuidedReviewScroll = min(maxScroll, m.GuidedReviewScroll+1)
	case "up":
		m.GuidedReviewScroll = max(0, m.GuidedReviewScroll-1)
	case "pgdown":
		maxScroll, pageSize := m.guidedReviewScrollMetrics()
		m.GuidedReviewScroll = min(maxScroll, m.GuidedReviewScroll+pageSize)
	case "pgup":
		_, pageSize := m.guidedReviewScrollMetrics()
		m.GuidedReviewScroll = max(0, m.GuidedReviewScroll-pageSize)
	case "home", "g":
		m.GuidedReviewScroll = 0
	case "end", "G":
		maxScroll, _ := m.guidedReviewScrollMetrics()
		m.GuidedReviewScroll = maxScroll
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
		next, err := m.saveGuidedReviewDecision(review.ActionKeep)
		if err != nil {
			m.fail(fmt.Errorf("kept %s but review progress was not saved: %w", item.Target.Name, err))
			return m, nil
		}
		m.GuidedReview = next
		m.GuidedReviewScroll = 0
		m.setStatus("kept " + item.Target.Name + " and advanced the review")
	case "l":
		if _, ok := m.GuidedReview.Current(); !ok {
			return m, nil
		}
		next, err := m.saveGuidedReviewDecision(review.ActionRevisit)
		if err != nil {
			m.fail(fmt.Errorf("revisit decision was not saved: %w", err))
			return m, nil
		}
		m.GuidedReview = next
		m.GuidedReviewScroll = 0
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

func (m *Model) saveGuidedReviewDecision(action review.Action) (review.Session, error) {
	next, err := m.GuidedReview.Decide(action)
	if err != nil {
		return m.GuidedReview, err
	}
	_, kept := m.Actions.GuidedReviewState()
	next = review.Resume(m.Findings, kept, next.State())
	if err := m.Actions.SaveGuidedReviewState(next.State()); err != nil {
		return next, err
	}
	return next, nil
}

func (m Model) renderGuidedReview(theme ui.Theme, width, height int) string {
	lines := m.guidedReviewLines(theme, max(20, width-8))
	panelWidth := max(1, width-2)
	panelHeight := max(1, height-2)
	maxScroll := max(0, len(lines)-panelHeight)
	start := min(max(0, m.GuidedReviewScroll), maxScroll)
	end := min(len(lines), start+panelHeight)
	visible := append([]string(nil), lines[start:end]...)
	content := strings.Join(ui.PadLines(visible, panelHeight), "\n")
	return theme.Panel.Width(panelWidth).Height(panelHeight).Render(content)
}

func (m Model) guidedReviewLines(theme ui.Theme, contentWidth int) []string {
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
		for _, line := range wrapComplete(m.GuidedReview.CompletionMessage(), contentWidth) {
			lines = append(lines, theme.Muted.Render(line))
		}
		return append(lines, "", theme.Muted.Render("Esc returns to findings. Press v later to start a new review."))
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
	for _, line := range wrapComplete(skillTarget(item.Target), contentWidth) {
		lines = append(lines, theme.Row.Render(line))
	}
	lines = append(lines, "", theme.Section.Render("Consequence"))
	for _, line := range wrapComplete(item.Consequence, contentWidth) {
		lines = append(lines, theme.Row.Render(line))
	}
	return append(lines, "", theme.Muted.Render("k keep logical skill · q quarantine exact install · l revisit later"))
}

func (m Model) guidedReviewScrollMetrics() (maxScroll, pageSize int) {
	width, height := m.dimensions()
	theme := ui.DefaultTheme()
	bodyHeight := max(8, height-3-len(m.feedbackLinesForView(theme, width, height)))
	panelHeight := max(1, bodyHeight-2)
	lineCount := len(m.guidedReviewLines(theme, max(20, width-8)))
	return max(0, lineCount-panelHeight), panelHeight
}

func appendWrappedBullet(lines []string, theme ui.Theme, value string, width int) []string {
	wrapped := wrapComplete(value, max(1, width-2))
	for i, line := range wrapped {
		prefix := "  "
		if i == 0 {
			prefix = "• "
		}
		lines = append(lines, theme.Row.Render(prefix+line))
	}
	return lines
}
