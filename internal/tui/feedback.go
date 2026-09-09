package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/ui"
)

const defaultErrorRecovery = "Review the error, then retry the action."

func (m *Model) setStatus(message string) {
	m.Status = message
	m.StatusError = strings.Contains(strings.ToLower(message), "error:")
	m.StatusRecovery = ""
	m.StatusContext = m.State
	m.FeedbackScroll = 0
	if m.StatusError {
		m.StatusRecovery = defaultErrorRecovery
	}
}

func (m *Model) dismissStatus() {
	m.Status = ""
	m.StatusError = false
	m.StatusRecovery = ""
	m.StatusContext = StateNormal
	m.FeedbackScroll = 0
	if m.State == StateFeedback {
		m.State = StateNormal
	}
}

func (m Model) renderFeedback(theme ui.Theme, width, height int) []string {
	if strings.TrimSpace(m.Status) == "" || height <= 0 {
		return nil
	}
	lines := m.feedbackContent(theme, max(1, width-4), true)
	control := m.feedbackControl(false)
	if len(lines)+1 <= height {
		return append(lines, theme.Muted.Render(control))
	}
	control = m.feedbackControl(true)
	if height == 1 {
		return []string{theme.Muted.Render(control)}
	}
	lines = lines[:max(0, height-2)]
	lines = append(lines, theme.Muted.Render("…"), theme.Muted.Render(control))
	return lines
}

func (m Model) feedbackControl(truncated bool) string {
	if m.State == StateInputRename {
		return "edit the name and press enter"
	}
	if m.State == StateNormal && truncated {
		return "enter details · x dismiss"
	}
	return "x dismiss"
}

func (m Model) feedbackContent(theme ui.Theme, width int, includeLabel bool) []string {
	label := theme.Success.Render("Status")
	messageStyle := theme.Status
	if m.StatusError {
		label = theme.BadgeDanger.Render("Error")
		messageStyle = theme.Danger
	}
	message := m.Status
	if m.StatusError && strings.HasPrefix(strings.ToLower(message), "error:") {
		message = strings.TrimSpace(message[len("error:"):])
	}
	lines := make([]string, 0)
	if includeLabel {
		lines = append(lines, label)
	}
	for _, line := range wrapComplete(message, width) {
		lines = append(lines, messageStyle.Render(line))
	}
	if m.StatusRecovery != "" {
		if !includeLabel {
			lines = append(lines, "")
		}
		for _, line := range wrapComplete("Recovery: "+m.StatusRecovery, width) {
			lines = append(lines, theme.Section.Render(line))
		}
	}
	return lines
}

func (m Model) renderFeedbackDetail(theme ui.Theme, width, height int) []string {
	content := m.feedbackContent(theme, width, false)
	viewportHeight := max(1, height-2)
	maxScroll := max(0, len(content)-viewportHeight)
	start := min(max(0, m.FeedbackScroll), maxScroll)
	end := min(len(content), start+viewportHeight)
	title := "STATUS DETAILS"
	if m.StatusError {
		title = "ERROR DETAILS"
	}
	position := fmt.Sprintf("Lines %d–%d of %d", start+1, end, len(content))
	lines := []string{theme.Badge.Render(title), theme.Muted.Render(position)}
	return append(lines, content[start:end]...)
}

func (m Model) feedbackScrollMetrics() (maxScroll, pageSize int) {
	width, height := m.dimensions()
	contentWidth := min(104, max(1, width-22))
	bodyHeight := max(8, height-3)
	viewportHeight := max(1, bodyHeight-6)
	lineCount := len(m.feedbackContent(ui.DefaultTheme(), contentWidth, false))
	return max(0, lineCount-viewportHeight), viewportHeight
}

func wrapComplete(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, word := range words {
			for lipgloss.Width(word) > width {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				chunk, rest := splitAtWidth(word, width)
				lines = append(lines, chunk)
				word = rest
			}
			if line == "" {
				line = word
				continue
			}
			if lipgloss.Width(line+" "+word) > width {
				lines = append(lines, line)
				line = word
			} else {
				line += " " + word
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitAtWidth(value string, width int) (string, string) {
	runes := []rune(value)
	for i := 1; i <= len(runes); i++ {
		if lipgloss.Width(string(runes[:i])) > width {
			return string(runes[:i-1]), string(runes[i-1:])
		}
	}
	return value, ""
}

func (m Model) renderHelp(theme ui.Theme, width int) []string {
	if m.Mode == ViewGuidedReview {
		lines := []string{
			theme.Badge.Render("GUIDED REVIEW HELP"),
			"",
			theme.Section.Render("One decision at a time"),
			theme.Key.Render("k") + " keep the logical skill name",
			theme.Key.Render("q") + " quarantine this exact install after confirmation",
			theme.Key.Render("l") + " revisit in a later review",
			theme.Key.Render("esc") + " save progress and return to findings",
			"",
			theme.Muted.Render("A completed scope does not mean the global inventory is clean."),
		}
		for i, line := range lines {
			lines[i] = ui.Truncate(line, width)
		}
		return lines
	}
	title := "FINDINGS HELP"
	viewKey := "s"
	viewLabel := "skills"
	if m.Mode == ViewSkills {
		title = "SKILL INVENTORY HELP"
		viewKey = "f"
		viewLabel = "findings"
	}
	lines := []string{
		theme.Badge.Render(title),
		"",
		theme.Section.Render("Navigation"),
		theme.Key.Render("↑↓/jk") + " move  " + theme.Key.Render("tab/shift+tab") + " cycle install",
		theme.Key.Render(viewKey) + " " + viewLabel + "  " + theme.Key.Render("r") + " density",
	}
	if m.Mode == ViewSkills {
		lines = append(lines, theme.Key.Render("enter")+" open skill story")
	}
	lines = append(lines,
		"",
		theme.Section.Render("Cleanup"),
		theme.Key.Render("ctrl+q")+" quarantine  "+theme.Key.Render("ctrl+d")+" delete  "+theme.Key.Render("ctrl+r")+" rename",
		theme.Key.Render("ctrl+k")+" keep  "+theme.Key.Render("ctrl+u")+" restore  "+theme.Key.Render("ctrl+b")+" batch duplicates",
	)
	if m.Mode == ViewFindings {
		lines = append(lines, theme.Key.Render("ctrl+g")+" ignore finding")
	}
	lines = append(lines,
		"",
		theme.Section.Render("Other"),
		theme.Key.Render("d")+" discover by task  "+theme.Key.Render("v")+" guided review",
		theme.Key.Render("m")+" draft merge  "+theme.Key.Render("q")+" quit",
		"",
		theme.Muted.Render("Press esc or ? to close help."),
	)
	for i, line := range lines {
		lines[i] = ui.Truncate(line, width)
	}
	return lines
}

func interactionTitle(state InteractionState, count int) string {
	switch state {
	case StateWriteGate:
		return "ALLOW WRITE ACCESS"
	case StateConfirmQuarantine:
		if count > 1 {
			return "QUARANTINE INSTALLS"
		}
		return "QUARANTINE INSTALL"
	case StateConfirmDelete:
		if count > 1 {
			return "DELETE INSTALLS PERMANENTLY"
		}
		return "DELETE INSTALL PERMANENTLY"
	case StatePreviewRename:
		return "RENAME INSTALL"
	case StateSelectInstall:
		return "CHOOSE EXACT INSTALL"
	case StateSelectRestore:
		return "RESTORE SKILL"
	case StateSelectBatchRoot:
		return "BATCH DUPLICATES BY ROOT"
	case StateSelectDraftSkills:
		return "DRAFT MERGED SKILL"
	case StateGeneratingDraft:
		return "GENERATING MERGE DRAFT"
	case StatePreviewDraft:
		return "READ-ONLY MERGE DRAFT"
	}
	return "ACTION"
}

func confirmationLabel(state InteractionState) string {
	switch state {
	case StateWriteGate:
		return "allow writes"
	case StateConfirmQuarantine:
		return "quarantine"
	case StateConfirmDelete:
		return "delete"
	case StatePreviewRename:
		return "rename"
	default:
		return "confirm"
	}
}
