package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/ui"
)

const defaultErrorRecovery = "Review the error, then retry the action."

func (m *Model) setStatus(message string) {
	m.Status = message
	m.StatusError = strings.Contains(strings.ToLower(message), "error:")
	m.StatusRecovery = ""
	if m.StatusError {
		m.StatusRecovery = defaultErrorRecovery
	}
}

func (m *Model) dismissStatus() {
	m.Status = ""
	m.StatusError = false
	m.StatusRecovery = ""
}

func (m Model) renderFeedback(theme ui.Theme, width int) []string {
	if strings.TrimSpace(m.Status) == "" {
		return nil
	}
	contentWidth := max(1, width-4)
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
	lines := []string{label}
	for _, line := range wrapComplete(message, contentWidth) {
		lines = append(lines, messageStyle.Render(line))
	}
	if m.StatusRecovery != "" {
		for _, line := range wrapComplete("Recovery: "+m.StatusRecovery, contentWidth) {
			lines = append(lines, theme.Section.Render(line))
		}
	}
	lines = append(lines, theme.Muted.Render("x dismiss"))
	return lines
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
		"",
		theme.Section.Render("Cleanup"),
		theme.Key.Render("ctrl+q") + " quarantine  " + theme.Key.Render("ctrl+d") + " delete  " + theme.Key.Render("ctrl+r") + " rename",
		theme.Key.Render("ctrl+k") + " keep  " + theme.Key.Render("ctrl+u") + " restore  " + theme.Key.Render("ctrl+b") + " batch duplicates",
	}
	if m.Mode == ViewFindings {
		lines = append(lines, theme.Key.Render("ctrl+g")+" ignore finding")
	}
	lines = append(lines,
		"",
		theme.Section.Render("Other"),
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
