package setup

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/ui"
)

type RootChoice struct {
	Path    string
	Exists  bool
	Trusted bool
}

type Model struct {
	Roots        []RootChoice
	HistoryJSONL []string
	LLMEnabled   bool
	HistoryScan  bool
	Cursor       int
	Done         bool
	Cancelled    bool
	Width        int
	Height       int
}

func New(roots []RootChoice, historyJSONL []string, cfg config.Config) Model {
	choices := make([]RootChoice, len(roots))
	for i, root := range roots {
		root.Trusted = cfg.IsTrusted(root.Path)
		choices[i] = root
	}
	return Model{Roots: choices, HistoryJSONL: historyJSONL, LLMEnabled: cfg.LLMAssisted, HistoryScan: cfg.HistoryScan}
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
		theme.Muted.Render(ui.Truncate("Choose exactly what unlearn may scan. Write access is still requested later per action.", contentWidth)),
		"",
		theme.Section.Render("Skill roots"),
	}
	for i, root := range m.Roots {
		lines = append(lines, m.rootLine(theme, i, root, contentWidth))
	}
	lines = append(lines, "", theme.Section.Render("Options"))
	lines = append(lines, m.optionLine(theme, len(m.Roots), m.LLMEnabled, "LLM-assisted summaries and semantic overlap", contentWidth))
	historyLabel := "Pi JSONL history evidence"
	if len(m.HistoryJSONL) == 0 {
		historyLabel += " · none discovered"
	} else {
		historyLabel += fmt.Sprintf(" · %d paths discovered · read only after opt-in", len(m.HistoryJSONL))
	}
	lines = append(lines, m.optionLine(theme, len(m.Roots)+1, m.HistoryScan, historyLabel, contentWidth))
	lines = ui.FitLines(lines, bodyHeight-2)
	panel := theme.Panel.Width(panelWidth - 2).Height(bodyHeight - 2).Render(strings.Join(ui.PadLines(lines, bodyHeight-2), "\n"))
	keybar := renderSetupKeybar(theme, width)
	return lipgloss.JoinVertical(lipgloss.Left, panel, keybar)
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
	cfg.LLMAssisted = m.LLMEnabled
	cfg.HistoryScan = m.HistoryScan
	if m.HistoryScan {
		cfg.HistoryJSONL = append([]string(nil), m.HistoryJSONL...)
	} else {
		cfg.HistoryJSONL = nil
	}
	cfg.SetupComplete = true
	return cfg
}

func (m Model) itemCount() int { return len(m.Roots) + 2 }

func (m *Model) toggleCurrent() {
	if m.Cursor < len(m.Roots) {
		m.Roots[m.Cursor].Trusted = !m.Roots[m.Cursor].Trusted
		return
	}
	if m.Cursor == len(m.Roots) {
		m.LLMEnabled = !m.LLMEnabled
		return
	}
	m.HistoryScan = !m.HistoryScan
}
