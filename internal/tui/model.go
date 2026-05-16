package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fsactions "github.com/mblarsen/unlearn/internal/actions"
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

type InteractionState int

const (
	StateNormal InteractionState = iota
	StateWriteGate
	StateConfirmQuarantine
	StateInputDelete
	StateInputRename
	StatePreviewRename
	StateInputRestore
)

type PendingAction int

const (
	ActionNone PendingAction = iota
	ActionQuarantine
	ActionDelete
	ActionRename
	ActionRestore
)

type Model struct {
	Skills   []inventory.Skill
	Findings []analysis.Finding
	Actions  ActionService
	Mode     ViewMode
	Density  Density
	Cursor   int
	Width    int
	Height   int

	State          InteractionState
	PendingAction  PendingAction
	PendingSkill   inventory.Skill
	PendingFinding analysis.Finding
	Input          string
	Message        string
	Status         string
	RenamePreview  fsactions.RenamePreview
}

func New(skills []inventory.Skill, findings []analysis.Finding) Model {
	return NewWithActions(skills, findings, NoopActionService{})
}

func NewWithActions(skills []inventory.Skill, findings []analysis.Finding, service ActionService) Model {
	if service == nil {
		service = NoopActionService{}
	}
	return Model{Skills: skills, Findings: findings, Actions: service, Mode: ViewFindings, Density: DensityCompact}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	case tea.KeyMsg:
		if m.State != StateNormal {
			return m.updateInteraction(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "K":
		m.keepSelected()
	case "I":
		m.ignoreSelectedFinding()
	case "Q":
		m.beginSkillAction(ActionQuarantine)
	case "D":
		m.beginSkillAction(ActionDelete)
	case "N":
		m.beginSkillAction(ActionRename)
	case "R":
		m.beginSkillAction(ActionRestore)
	}
	return m, nil
}

func (m Model) updateInteraction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.State {
	case StateWriteGate:
		return m.updateWriteGate(msg)
	case StateConfirmQuarantine:
		return m.updateQuarantineConfirm(msg)
	case StateInputDelete, StateInputRename, StateInputRestore:
		return m.updateInput(msg)
	case StatePreviewRename:
		return m.updateRenamePreview(msg)
	default:
		m.resetInteraction()
		return m, nil
	}
}

func (m Model) updateWriteGate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if err := m.Actions.AllowWrite(m.PendingSkill.Root); err != nil {
			m.fail(err)
			return m, nil
		}
		m.continuePendingAfterWriteGate()
	case "n", "N", "esc":
		m.cancel("write permission declined")
	}
	return m, nil
}

func (m Model) updateQuarantineConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		dest, err := m.Actions.Quarantine(m.PendingSkill)
		if err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("quarantined %s -> %s", m.PendingSkill.Name, dest))
		}
	case "n", "N", "esc":
		m.cancel("quarantine cancelled")
	}
	return m, nil
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.cancel("action cancelled")
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.Input) > 0 {
			m.Input = m.Input[:len(m.Input)-1]
		}
		return m, nil
	case tea.KeyEnter:
		m.submitInput()
		return m, nil
	case tea.KeyRunes:
		m.Input += msg.String()
	}
	return m, nil
}

func (m Model) updateRenamePreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.RenamePreview.Warn != "" {
			m.complete(m.RenamePreview.Warn + "; suggested action: quarantine")
			return m, nil
		}
		preview, err := m.Actions.Rename(m.PendingSkill, m.Input)
		if err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("renamed %s -> %s", preview.OldPath, preview.NewPath))
		}
	case "n", "N", "esc":
		m.cancel("rename cancelled")
	}
	return m, nil
}

func (m *Model) beginSkillAction(action PendingAction) {
	skill, ok := m.selectedSkill()
	if !ok {
		m.Status = "no skill selected"
		return
	}
	m.PendingAction = action
	m.PendingSkill = skill
	if !m.Actions.CanWrite(skill.Root) {
		m.State = StateWriteGate
		m.Message = fmt.Sprintf("Allow unlearn to modify root %s? y/n", skill.Root)
		return
	}
	m.continuePendingAfterWriteGate()
}

func (m *Model) continuePendingAfterWriteGate() {
	switch m.PendingAction {
	case ActionQuarantine:
		m.State = StateConfirmQuarantine
		m.Message = fmt.Sprintf("Quarantine %s? y/n", m.PendingSkill.Name)
	case ActionDelete:
		m.State = StateInputDelete
		m.Input = ""
		m.Message = fmt.Sprintf("Type %s to permanently delete active skill", m.PendingSkill.Name)
	case ActionRename:
		m.State = StateInputRename
		m.Input = ""
		m.Message = fmt.Sprintf("New name for %s", m.PendingSkill.Name)
	case ActionRestore:
		m.State = StateInputRestore
		m.Input = ""
		m.Message = fmt.Sprintf("Quarantined skill name to restore into %s", m.PendingSkill.Root)
	default:
		m.resetInteraction()
	}
}

func (m *Model) submitInput() {
	switch m.State {
	case StateInputDelete:
		if err := m.Actions.Delete(m.PendingSkill, m.Input); err != nil {
			m.fail(err)
		} else {
			m.complete("deleted " + m.PendingSkill.Name)
		}
	case StateInputRename:
		if strings.TrimSpace(m.Input) == "" {
			m.Status = "rename requires a new name"
			return
		}
		m.RenamePreview = m.Actions.PreviewRename(m.PendingSkill, m.Input)
		m.State = StatePreviewRename
		m.Message = fmt.Sprintf("Rename dry run: %s -> %s; %s. Confirm? y/n", m.RenamePreview.OldPath, m.RenamePreview.NewPath, m.RenamePreview.Frontmatter)
		if m.RenamePreview.Warn != "" {
			m.Message = "Warning: " + m.RenamePreview.Warn + "; suggested action: quarantine. Press y to acknowledge or n to cancel."
		}
	case StateInputRestore:
		if strings.TrimSpace(m.Input) == "" {
			m.Status = "restore requires a skill name"
			return
		}
		dest, err := m.Actions.Restore(m.Input, m.PendingSkill.Root)
		if err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("restored %s -> %s", m.Input, dest))
		}
	}
}

func (m *Model) keepSelected() {
	skill, ok := m.selectedSkill()
	if !ok {
		m.Status = "no skill selected"
		return
	}
	if err := m.Actions.KeepSkill(skill); err != nil {
		m.fail(err)
		return
	}
	m.Status = "kept " + skill.Name
}

func (m *Model) ignoreSelectedFinding() {
	if m.Mode != ViewFindings || len(m.Findings) == 0 {
		m.Status = "ignore finding is only available in findings view"
		return
	}
	finding := m.Findings[m.Cursor]
	if err := m.Actions.IgnoreFinding(finding); err != nil {
		m.fail(err)
		return
	}
	m.Status = "ignored " + finding.Title
}

func (m *Model) selectedSkill() (inventory.Skill, bool) {
	if m.itemCount() == 0 {
		return inventory.Skill{}, false
	}
	if m.Mode == ViewSkills {
		return m.Skills[m.Cursor], true
	}
	finding := m.Findings[m.Cursor]
	if len(finding.Skills) == 0 {
		return inventory.Skill{}, false
	}
	return finding.Skills[0], true
}

func (m *Model) resetInteraction() {
	m.State = StateNormal
	m.PendingAction = ActionNone
	m.PendingSkill = inventory.Skill{}
	m.PendingFinding = analysis.Finding{}
	m.Input = ""
	m.Message = ""
	m.RenamePreview = fsactions.RenamePreview{}
}

func (m *Model) complete(status string) {
	m.resetInteraction()
	m.Status = status
}

func (m *Model) cancel(status string) {
	m.resetInteraction()
	m.Status = status
}

func (m *Model) fail(err error) {
	m.resetInteraction()
	m.Status = "error: " + err.Error()
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
	if m.State != StateNormal {
		lines = append(lines, m.Message)
		if m.Input != "" {
			lines = append(lines, "> "+m.Input)
		}
		return strings.Join(lines, "\n")
	}
	if m.Status != "" {
		lines = append(lines, m.Status, "")
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
	)
	return strings.Join(lines, "\n")
}

func (m Model) keyBar() string {
	keys := []string{"j/k/↑/↓ move", "r density"}
	if m.State != StateNormal {
		keys = m.interactionKeys()
		return lipgloss.NewStyle().Reverse(true).Render(strings.Join(keys, " · "))
	}
	if m.Mode == ViewFindings {
		keys = append(keys, "s skill inventory")
	} else {
		keys = append(keys, "f findings")
	}
	keys = append(keys, m.availableActionKeys()...)
	keys = append(keys, "q quit")
	return lipgloss.NewStyle().Reverse(true).Render(strings.Join(keys, " · "))
}

func (m Model) interactionKeys() []string {
	switch m.State {
	case StateWriteGate, StateConfirmQuarantine, StatePreviewRename:
		return []string{"y confirm", "n cancel", "esc cancel"}
	case StateInputDelete, StateInputRename, StateInputRestore:
		return []string{"type", "enter submit", "esc cancel"}
	default:
		return []string{"esc cancel"}
	}
}

func (m Model) availableActionKeys() []string {
	if m.itemCount() == 0 {
		return nil
	}
	keys := []string{"enter inspect"}
	if m.Mode == ViewFindings {
		keys = append(keys, "I ignore finding")
	}
	keys = append(keys, "K keep", "Q quarantine", "D delete", "N rename", "R restore")
	return keys
}

func (m Model) itemCount() int {
	if m.Mode == ViewFindings {
		return len(m.Findings)
	}
	return len(m.Skills)
}
