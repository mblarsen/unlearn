package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/discovery"
	"github.com/mblarsen/unlearn/internal/ui"
)

type discoveryState struct {
	Query  string
	Result discovery.Result
	Cursor int
	Scroll int
}

func (m *Model) beginDiscovery() {
	m.Discovery = discoveryState{}
	m.State = StateDiscoveryQuery
}

func (m Model) updateDiscovery(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.State {
	case StateDiscoveryQuery:
		switch msg.Type {
		case tea.KeyEsc:
			m.State = StateNormal
		case tea.KeyEnter:
			m.Discovery.Result = discovery.Search(m.Discovery.Query, m.Skills, m.Findings)
			m.Discovery.Cursor = 0
			m.Discovery.Scroll = 0
			m.State = StateDiscoveryResults
		case tea.KeyBackspace, tea.KeyDelete:
			runes := []rune(m.Discovery.Query)
			if len(runes) > 0 {
				m.Discovery.Query = string(runes[:len(runes)-1])
			}
		case tea.KeySpace:
			m.Discovery.Query += " "
		case tea.KeyRunes:
			m.Discovery.Query += string(msg.Runes)
		}
	case StateDiscoveryResults:
		switch msg.String() {
		case "esc", "q":
			m.State = StateNormal
		case "e":
			m.State = StateDiscoveryQuery
		case "j", "down":
			if m.Discovery.Cursor < len(m.Discovery.Result.Matches)-1 {
				m.Discovery.Cursor++
			}
		case "k", "up":
			if m.Discovery.Cursor > 0 {
				m.Discovery.Cursor--
			}
		case "enter":
			if len(m.Discovery.Result.Matches) > 0 {
				m.Discovery.Scroll = 0
				m.State = StateDiscoveryInspect
			}
		}
	case StateDiscoveryInspect:
		match := m.selectedDiscoveryMatch()
		contentWidth, contentHeight := m.discoveryContentDimensions()
		maxScroll := max(0, len(m.discoveryInspectLines(ui.DefaultTheme(), match, contentWidth))-contentHeight)
		switch msg.String() {
		case "esc", "q":
			m.State = StateDiscoveryResults
		case "e":
			m.State = StateDiscoveryQuery
		case "j", "down":
			m.Discovery.Scroll = min(maxScroll, m.Discovery.Scroll+1)
		case "k", "up":
			m.Discovery.Scroll = max(0, m.Discovery.Scroll-1)
		case "pgdown", "ctrl+d":
			m.Discovery.Scroll = min(maxScroll, m.Discovery.Scroll+8)
		case "pgup", "ctrl+u":
			m.Discovery.Scroll = max(0, m.Discovery.Scroll-8)
		}
	}
	return m, nil
}

func (m Model) selectedDiscoveryMatch() discovery.Match {
	if len(m.Discovery.Result.Matches) == 0 {
		return discovery.Match{}
	}
	cursor := min(max(0, m.Discovery.Cursor), len(m.Discovery.Result.Matches)-1)
	return m.Discovery.Result.Matches[cursor]
}

func (m Model) renderDiscovery(theme ui.Theme, width, height int) []string {
	switch m.State {
	case StateDiscoveryQuery:
		lines := []string{
			theme.Badge.Render("WHAT HELPS ME DO THIS?"),
			"",
			theme.Section.Render("Describe the task"),
			theme.Muted.Render("Search uses installed skill names and descriptions only."),
			"",
			theme.Accent.Render("› ") + visibleDiscoveryInput(m.Discovery.Query, width-2),
			"",
			theme.Muted.Render("Example: review a pull request for security issues"),
		}
		return truncateLines(lines, width)
	case StateDiscoveryResults:
		return m.renderDiscoveryResults(theme, width, height)
	case StateDiscoveryInspect:
		return m.renderDiscoveryInspect(theme, width, height)
	default:
		return nil
	}
}

func (m Model) renderDiscoveryResults(theme ui.Theme, width, height int) []string {
	result := m.Discovery.Result
	lines := []string{theme.Badge.Render("DISCOVERY RESULTS")}
	for _, line := range wrapPreservingText("Task: "+result.Query, width) {
		lines = append(lines, theme.Muted.Render(line))
	}
	lines = append(lines, "")
	if len(result.Matches) == 0 {
		lines = append(lines, theme.Section.Render(result.Message), "", theme.Muted.Render("Press e to edit the task or esc to return to the dashboard."))
		return truncateLines(lines, width)
	}
	lines = append(lines, theme.Section.Render(fmt.Sprintf("%d matching installed %s", len(result.Matches), discoverySkillWord(len(result.Matches)))))
	rows := make([]string, 0, len(result.Matches))
	selectedLine := 0
	for i, match := range result.Matches {
		prefix := "  "
		style := theme.Row
		if i == m.Discovery.Cursor {
			prefix = "▸ "
			style = theme.SelectedRow.Width(width)
			selectedLine = i
		}
		meta := fmt.Sprintf("%d %s", len(match.Installs), installWord(len(match.Installs)))
		if match.Weak {
			meta += " · weak term match"
		}
		rows = append(rows, style.Render(ui.Truncate(prefix+padBetween(match.Name, meta, width-2), width)))
	}
	rows = windowLines(rows, max(1, height-len(lines)), selectedLine)
	lines = append(lines, rows...)
	return truncateLines(lines, width)
}

func (m Model) renderDiscoveryInspect(theme ui.Theme, width, height int) []string {
	match := m.selectedDiscoveryMatch()
	all := m.discoveryInspectLines(theme, match, width)
	available := max(1, height)
	start := min(m.Discovery.Scroll, max(0, len(all)-available))
	end := min(len(all), start+available)
	lines := append([]string(nil), all[start:end]...)
	if start > 0 && len(lines) > 0 {
		lines[0] = theme.Muted.Render("… above")
	}
	if end < len(all) && len(lines) > 0 {
		lines[len(lines)-1] = theme.Muted.Render("… more · j/k or PgUp/PgDn")
	}
	return truncateLines(lines, width)
}

func (m Model) discoveryInspectLines(theme ui.Theme, match discovery.Match, width int) []string {
	lines := []string{theme.Badge.Render("SKILL MATCH")}
	for _, line := range wrapPreservingText(match.Name, width) {
		lines = append(lines, theme.Accent.Render(line))
	}
	lines = append(lines, "")
	if match.Description != "" {
		lines = append(lines, theme.Section.Render("Observed description"))
		lines = appendWrappedDiscoveryFact(lines, theme.Row, match.Description, width)
		lines = append(lines, "")
	}
	lines = append(lines, theme.Section.Render("Why it matched"))
	for _, reason := range match.Reasons {
		lines = appendWrappedDiscoveryFact(lines, theme.Row, "• "+reason, width)
	}
	if match.Weak {
		lines = appendWrappedDiscoveryFact(lines, theme.Muted, "Weak term match: only one distinctive task term matched.", width)
	}
	lines = appendWrappedDiscoveryFact(lines, theme.Muted, "Invocation: "+match.Invocation, width)
	if len(match.OverlapWith) > 0 {
		lines = appendWrappedDiscoveryFact(lines, theme.Muted, "Known overlap in these results: "+strings.Join(match.OverlapWith, ", "), width)
	}
	lines = append(lines, "", theme.Section.Render("Exact installs"))
	for _, install := range match.Installs {
		status := "Available in inventory"
		if install.Missing {
			status = "Missing at scan time"
		}
		lines = appendWrappedDiscoveryFact(lines, theme.Row, "• "+install.Path, width)
		lines = appendWrappedDiscoveryFact(lines, theme.Muted, "  "+status, width)
		lines = appendWrappedDiscoveryFact(lines, theme.Muted, "  "+installAccess(install), width)
	}
	lines = append(lines, "")
	lines = appendWrappedDiscoveryFact(lines, theme.Muted, "A match does not activate a skill or prove that it can complete the task.", width)
	return lines
}

func appendWrappedDiscoveryFact(lines []string, style lipgloss.Style, value string, width int) []string {
	for _, line := range wrapPreservingText(value, width) {
		lines = append(lines, style.Render(line))
	}
	return lines
}

func (m Model) discoveryContentDimensions() (int, int) {
	modalWidth := min(max(72, m.Width-16), 112)
	bodyHeight := max(10, m.Height-3)
	return max(1, modalWidth-6), max(1, bodyHeight-4)
}

func installAccess(install discovery.Install) string {
	if !install.AccessKnown {
		return "Agent access: unknown"
	}
	if len(install.ActiveAgents) > 0 {
		return "Accessible to: " + strings.Join(install.ActiveAgents, ", ")
	}
	if len(install.InactiveAgents) > 0 {
		return "Known only for inactive agents: " + strings.Join(install.InactiveAgents, ", ")
	}
	return "No selected agent has known access"
}

func visibleDiscoveryInput(value string, width int) string {
	if width <= 0 || ui.Truncate(value, width) == value {
		return ui.Truncate(value, width)
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width("…"+string(runes)) > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

func discoverySkillWord(count int) string {
	if count == 1 {
		return "skill"
	}
	return "skills"
}

func truncateLines(lines []string, width int) []string {
	for i := range lines {
		lines[i] = ui.Truncate(lines[i], width)
	}
	return lines
}
