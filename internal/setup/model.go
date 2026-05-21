package setup

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/ui"
)

type RootChoice struct {
	Path    string
	Exists  bool
	Trusted bool
}

type AgentChoice struct {
	ID       string
	Name     string
	Active   bool
	Inactive bool
	Detected bool
}

type Model struct {
	Roots         []RootChoice
	Agents        []AgentChoice
	HistoryJSONL  []string
	HistorySQLite []string
	LLMEnabled    bool
	HistoryScan   bool
	Cursor        int
	Done          bool
	Cancelled     bool
	Width         int
	Height        int
}

func New(roots []RootChoice, historyJSONL []string, historySQLite []string, cfg config.Config, statuses []inventory.AgentStatus) Model {
	choices := make([]RootChoice, len(roots))
	for i, root := range roots {
		root.Trusted = cfg.IsTrusted(root.Path)
		choices[i] = root
	}
	agents := agentChoices(cfg, statuses)
	return Model{Roots: choices, Agents: agents, HistoryJSONL: historyJSONL, HistorySQLite: historySQLite, LLMEnabled: cfg.LLMAssisted, HistoryScan: cfg.HistoryScan}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.Cancelled = true
			return m, tea.Quit
		case "enter":
			m.Done = true
			return m, tea.Quit
		case "j", "down":
			if m.Cursor < m.itemCount()-1 {
				m.Cursor++
			}
		case "k", "up":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case " ":
			m.toggleCurrent()
		case "l":
			m.LLMEnabled = !m.LLMEnabled
		case "h":
			m.HistoryScan = !m.HistoryScan
		}
	}
	return m, nil
}

func (m Model) View() string {
	theme := ui.DefaultTheme()
	width := m.Width
	if width <= 0 {
		width = 90
	}
	height := m.Height
	if height <= 0 {
		height = 25
	}
	if width < 70 {
		width = 70
	}
	panelWidth := width - 4
	bodyHeight := height - 4
	if bodyHeight < 12 {
		bodyHeight = 12
	}
	contentWidth := panelWidth - 4
	lines := []string{
		theme.AppTitle.Render("unlearn setup") + theme.Muted.Render("  first launch permissions"),
		theme.Muted.Render(ui.Truncate("Choose roots and active harnesses. Inactive harness roots become cleanup candidates.", contentWidth)),
		"",
		theme.Section.Render("Agent harnesses"),
	}
	itemLines := make([]int, m.itemCount())
	for i, agent := range m.Agents {
		itemLines[i] = len(lines)
		lines = append(lines, m.agentLine(theme, i, agent, contentWidth))
	}
	lines = append(lines, "", theme.Section.Render("Skill roots"))
	for i, root := range m.Roots {
		itemIndex := len(m.Agents) + i
		itemLines[itemIndex] = len(lines)
		lines = append(lines, m.rootLine(theme, itemIndex, root, contentWidth))
	}
	optionStart := len(m.Agents) + len(m.Roots)
	lines = append(lines, "", theme.Section.Render("Options"))
	itemLines[optionStart] = len(lines)
	lines = append(lines, m.optionLine(theme, optionStart, m.LLMEnabled, "LLM-assisted summaries and semantic overlap", contentWidth))
	historyLabel := "Agent history evidence"
	if len(m.HistoryJSONL) == 0 && len(m.HistorySQLite) == 0 {
		historyLabel += " · none discovered"
	} else {
		historyLabel += fmt.Sprintf(" · %d JSONL · %d SQLite discovered · read only after opt-in", len(m.HistoryJSONL), len(m.HistorySQLite))
	}
	itemLines[optionStart+1] = len(lines)
	lines = append(lines, m.optionLine(theme, optionStart+1, m.HistoryScan, historyLabel, contentWidth))
	visibleLines := setupVisibleLines(lines, itemLines, m.Cursor, bodyHeight-2)
	panel := theme.Panel.Width(panelWidth - 2).Height(bodyHeight - 2).Render(strings.Join(ui.PadLines(visibleLines, bodyHeight-2), "\n"))
	keybar := renderSetupKeybar(theme, width)
	return lipgloss.JoinVertical(lipgloss.Left, panel, keybar)
}

func (m Model) agentLine(theme ui.Theme, index int, agent AgentChoice, width int) string {
	state := "off"
	status := theme.Muted.Render("off")
	mark := "□"
	if agent.Active {
		state = "active"
		status = theme.Success.Render("active")
		mark = "■"
	} else if agent.Inactive {
		state = "inactive"
		status = theme.Warning.Render("inactive")
		mark = "◧"
	}
	if agent.Detected {
		state += " · detected"
		status = status + theme.Muted.Render(" · detected")
	}
	rowWidth := width - 2
	left := fmt.Sprintf("%s %s", mark, agent.Name)
	line := padBetween(ui.Truncate(left, rowWidth-lipgloss.Width(state)-1), status, rowWidth)
	if index == m.Cursor {
		return theme.SelectedRow.Width(width).Render("▸ " + line)
	}
	return theme.Row.Render("  " + line)
}

func (m Model) rootLine(theme ui.Theme, index int, root RootChoice, width int) string {
	mark := "□"
	if root.Trusted {
		mark = "■"
	}
	statusText := "missing"
	status := theme.Muted.Render(statusText)
	if root.Exists && root.Trusted {
		statusText = "trusted"
		status = theme.Success.Render(statusText)
	} else if root.Exists {
		statusText = "not trusted"
		status = theme.Warning.Render(statusText)
	}
	rowWidth := width - 2
	left := fmt.Sprintf("%s %s", mark, root.Path)
	line := padBetween(ui.Truncate(left, rowWidth-lipgloss.Width(statusText)-1), status, rowWidth)
	if index == m.Cursor {
		return theme.SelectedRow.Width(width).Render("▸ " + line)
	}
	return theme.Row.Render("  " + line)
}

func (m Model) optionLine(theme ui.Theme, index int, enabled bool, label string, width int) string {
	mark := "□"
	if enabled {
		mark = "■"
	}
	line := ui.Truncate(fmt.Sprintf("%s %s", mark, label), width-2)
	if index == m.Cursor {
		return theme.SelectedRow.Width(width).Render("▸ " + line)
	}
	return theme.Row.Render("  " + line)
}

func agentChoices(cfg config.Config, statuses []inventory.AgentStatus) []AgentChoice {
	active := map[string]bool{}
	inactive := map[string]bool{}
	if cfg.HasAgentSelection() {
		for _, id := range cfg.ActiveAgents {
			active[id] = true
		}
		for _, id := range cfg.InactiveAgents {
			inactive[id] = true
		}
	}
	choices := []AgentChoice{}
	for _, status := range statuses {
		if !status.ShowInSetup {
			continue
		}
		choice := AgentChoice{ID: status.ID, Name: status.DisplayName, Detected: status.Installed}
		if cfg.HasAgentSelection() {
			choice.Active = active[status.ID]
			choice.Inactive = inactive[status.ID]
		} else if status.Installed || defaultActiveAgent(status.ID) {
			choice.Active = true
		}
		choices = append(choices, choice)
	}
	return choices
}

func defaultActiveAgent(id string) bool {
	switch id {
	case "pi", "codex", "opencode", "cline":
		return true
	default:
		return false
	}
}

func setupVisibleLines(lines []string, itemLines []int, cursor int, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	selectedLine := 0
	if cursor >= 0 && cursor < len(itemLines) {
		selectedLine = itemLines[cursor]
	}
	selectedOffset := height - 1
	if selectedLine < len(lines)-1 {
		selectedOffset = height - 2
	}
	start := selectedLine - selectedOffset
	if start < 0 {
		start = 0
	}
	if start > len(lines)-height {
		start = len(lines) - height
	}
	out := append([]string(nil), lines[start:start+height]...)
	if start > 0 {
		out[0] = ui.Truncate("… above", lipgloss.Width(out[0]))
	}
	if start+height < len(lines) {
		out[height-1] = ui.Truncate("… more", lipgloss.Width(out[height-1]))
	}
	return out
}

func padBetween(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+rightWidth+1 >= width {
		if rightWidth+2 >= width {
			return ui.Truncate(left, width)
		}
		return ui.Truncate(left, width-rightWidth-1) + " " + right
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

func renderSetupKeybar(theme ui.Theme, width int) string {
	parts := []string{
		theme.Key.Render("space") + " toggle",
		theme.Key.Render("j/k") + " move",
		theme.Key.Render("l") + " llm",
		theme.Key.Render("h") + " history",
		theme.Key.Render("enter") + " continue",
		theme.Key.Render("q") + " cancel",
	}
	limit := width - 2
	line := ""
	hidden := false
	for _, part := range parts {
		candidate := part
		if line != "" {
			candidate = line + "  " + part
		}
		if lipgloss.Width(candidate) > limit {
			hidden = true
			break
		}
		line = candidate
	}
	if hidden && lipgloss.Width(line+"  …") <= limit {
		line += theme.Muted.Render("  …")
	}
	return theme.Keybar.Width(limit).Render(ui.Truncate(line, limit))
}

func (m Model) ApplyTo(cfg config.Config) config.Config {
	for _, root := range m.Roots {
		if root.Trusted {
			cfg.TrustRoot(root.Path)
		}
	}
	cfg.ActiveAgents = nil
	cfg.InactiveAgents = nil
	for _, agent := range m.Agents {
		if agent.Active {
			cfg.ActiveAgents = append(cfg.ActiveAgents, agent.ID)
		} else if agent.Inactive {
			cfg.InactiveAgents = append(cfg.InactiveAgents, agent.ID)
		}
	}
	cfg.LLMAssisted = m.LLMEnabled
	cfg.HistoryScan = m.HistoryScan
	if m.HistoryScan {
		cfg.HistoryJSONL = append([]string(nil), m.HistoryJSONL...)
		cfg.HistorySQLite = append([]string(nil), m.HistorySQLite...)
	} else {
		cfg.HistoryJSONL = nil
		cfg.HistorySQLite = nil
	}
	cfg.SetupComplete = true
	return cfg
}

func (m Model) itemCount() int { return len(m.Agents) + len(m.Roots) + 2 }

func (m *Model) toggleCurrent() {
	if m.Cursor < len(m.Agents) {
		agent := &m.Agents[m.Cursor]
		switch {
		case agent.Active:
			agent.Active = false
			agent.Inactive = true
		case agent.Inactive:
			agent.Inactive = false
		default:
			agent.Active = true
		}
		return
	}
	rootIndex := m.Cursor - len(m.Agents)
	if rootIndex >= 0 && rootIndex < len(m.Roots) {
		m.Roots[rootIndex].Trusted = !m.Roots[rootIndex].Trusted
		return
	}
	optionIndex := m.Cursor - len(m.Agents) - len(m.Roots)
	if optionIndex == 0 {
		m.LLMEnabled = !m.LLMEnabled
		return
	}
	m.HistoryScan = !m.HistoryScan
}
