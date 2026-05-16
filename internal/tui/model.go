package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

type ViewMode int

const (
	ViewFindings ViewMode = iota
	ViewSkills
)

type Density int

const (
	DensityCompact Density = iota
	DensityRich
)

type Model struct {
	Skills   []inventory.Skill
	Findings []analysis.Finding
	Mode     ViewMode
	Density  Density
	Cursor   int
	Width    int
	Height   int
}

func New(skills []inventory.Skill, findings []analysis.Finding) Model {
	return Model{Skills: skills, Findings: findings, Mode: ViewFindings, Density: DensityCompact}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.Cursor < m.itemCount()-1 {
				m.Cursor++
			}
		case "k", "up":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "s":
			m.Mode = ViewSkills
			m.Cursor = 0
		case "f":
			m.Mode = ViewFindings
			m.Cursor = 0
		case "r":
			if m.Density == DensityCompact {
				m.Density = DensityRich
			} else {
				m.Density = DensityCompact
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.Width == 0 {
		m.Width = 100
	}
	leftWidth := m.Width / 2
	if leftWidth < 35 {
		leftWidth = 35
	}
	left := lipgloss.NewStyle().Width(leftWidth).Render(m.listView())
	right := lipgloss.NewStyle().Width(m.Width - leftWidth - 2).Render(m.detailView())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	return body + "\n" + m.keyBar()
}

func (m Model) listView() string {
	title := "Findings"
	if m.Mode == ViewSkills {
		title = "Skills"
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Render(title)}
	if m.itemCount() == 0 {
		lines = append(lines, "No items")
		return strings.Join(lines, "\n")
	}
	if m.Mode == ViewFindings {
		for i, finding := range m.Findings {
			prefix := "  "
			if i == m.Cursor {
				prefix = "› "
			}
			line := fmt.Sprintf("%s%s (%d)", prefix, finding.Title, len(finding.Skills))
			if m.Density == DensityRich && len(finding.Reasons) > 0 {
				line += " — " + finding.Reasons[0]
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	for i, skill := range m.Skills {
		prefix := "  "
		if i == m.Cursor {
			prefix = "› "
		}
		line := fmt.Sprintf("%s%s [%s]", prefix, skill.Name, skill.Kind)
		if m.Density == DensityRich {
			line += fmt.Sprintf(" — %d-%d tokens · %s", skill.LowerTokens, skill.UpperTokens, skill.ActivationRisk)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) detailView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Render("Details")}
	if m.itemCount() == 0 {
		return strings.Join(lines, "\n")
	}
	if m.Mode == ViewFindings {
		finding := m.Findings[m.Cursor]
		lines = append(lines, finding.Title, "Why flagged:")
		for _, reason := range finding.Reasons {
			lines = append(lines, "- "+reason)
		}
		for _, skill := range finding.Skills {
			lines = append(lines, fmt.Sprintf("\n%s", skill.Name), fmt.Sprintf("tokens: %d-%d", skill.LowerTokens, skill.UpperTokens), "activation risk: "+skill.ActivationRisk, "provenance: "+skill.Provenance)
		}
		return strings.Join(lines, "\n")
	}
	skill := m.Skills[m.Cursor]
	lines = append(lines,
		skill.Name,
		skill.Description,
		fmt.Sprintf("tokens: %d-%d", skill.LowerTokens, skill.UpperTokens),
		"activation risk: "+skill.ActivationRisk,
		"provenance: "+skill.Provenance,
		"usage evidence: not scanned",
		"actions: inspect · keep · ignore finding · quarantine · delete · rename",
	)
	return strings.Join(lines, "\n")
}

func (m Model) keyBar() string {
	keys := "j/k/↑/↓ move · r density · q quit"
	if m.Mode == ViewFindings {
		keys = "j/k/↑/↓ move · s skill inventory · r density · q quit"
	} else {
		keys = "j/k/↑/↓ move · f findings · r density · q quit"
	}
	return lipgloss.NewStyle().Reverse(true).Render(keys)
}

func (m Model) itemCount() int {
	if m.Mode == ViewFindings {
		return len(m.Findings)
	}
	return len(m.Skills)
}
