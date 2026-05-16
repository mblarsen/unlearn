package setup

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/config"
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
	lines := []string{lipgloss.NewStyle().Bold(true).Render("unlearn setup"), "", "Skill roots:"}
	for i, root := range m.Roots {
		cursor := "  "
		if i == m.Cursor {
			cursor = "› "
		}
		mark := "[ ]"
		if root.Trusted {
			mark = "[x]"
		}
		status := "missing"
		if root.Exists {
			status = "scan trusted"
			if !root.Trusted {
				status = "not yet trusted"
			}
		}
		lines = append(lines, fmt.Sprintf("%s%s %s  %s", cursor, mark, root.Path, status))
	}
	lines = append(lines, "", "Options:")
	lines = append(lines, optionLine(m.Cursor == len(m.Roots), m.LLMEnabled, "Enable LLM-assisted summaries and overlap detection"))
	historyLabel := "Scan local Pi JSONL histories for actual invocation evidence"
	if len(m.HistoryJSONL) == 0 {
		historyLabel += " (none discovered)"
	} else {
		historyLabel += fmt.Sprintf(" (%d files discovered; paths only so far)", len(m.HistoryJSONL))
	}
	lines = append(lines, optionLine(m.Cursor == len(m.Roots)+1, m.HistoryScan, historyLabel))
	lines = append(lines, "", "space toggle · l LLM · h history · enter continue · q cancel")
	return strings.Join(lines, "\n")
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

func optionLine(selected bool, enabled bool, label string) string {
	cursor := "  "
	if selected {
		cursor = "› "
	}
	mark := "[ ]"
	if enabled {
		mark = "[x]"
	}
	return fmt.Sprintf("%s%s %s", cursor, mark, label)
}
