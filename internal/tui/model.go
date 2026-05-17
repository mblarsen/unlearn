package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/ui"
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
	Skills      []inventory.Skill
	SkillGroups []skillGroup
	Findings    []analysis.Finding
	Actions     ActionService
	Mode        ViewMode
	Density     Density
	Cursor      int
	Width       int
	Height      int

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
	return Model{Skills: skills, SkillGroups: groupedSkills(skills), Findings: findings, Actions: service, Mode: ViewFindings, Density: DensityCompact}
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
		m.Message = fmt.Sprintf("Allow write access for %s?", skill.Root)
		return
	}
	m.continuePendingAfterWriteGate()
}

func (m *Model) continuePendingAfterWriteGate() {
	switch m.PendingAction {
	case ActionQuarantine:
		m.State = StateConfirmQuarantine
		m.Message = fmt.Sprintf("Move %s into unlearn quarantine?", m.PendingSkill.Name)
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
		m.Message = fmt.Sprintf("Rename dry run: %s → %s; %s", m.RenamePreview.OldPath, m.RenamePreview.NewPath, m.RenamePreview.Frontmatter)
		if m.RenamePreview.Warn != "" {
			m.Message = "Warning: " + m.RenamePreview.Warn + "; suggested action: quarantine."
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
	finding, ok := m.selectedFinding()
	if !ok {
		m.Status = "no finding selected"
		return
	}
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
		return m.SkillGroups[m.Cursor].Representative, true
	}
	finding, ok := m.selectedFinding()
	if !ok || len(finding.Skills) == 0 {
		return inventory.Skill{}, false
	}
	return finding.Skills[0], true
}

func (m Model) selectedFinding() (analysis.Finding, bool) {
	if m.Mode != ViewFindings || len(m.Findings) == 0 || m.Cursor < 0 || m.Cursor >= len(m.Findings) {
		return analysis.Finding{}, false
	}
	idx := 0
	for _, section := range groupedFindings(m.Findings) {
		for _, finding := range section.Findings {
			if idx == m.Cursor {
				return finding, true
			}
			idx++
		}
	}
	return analysis.Finding{}, false
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
	width, height := m.dimensions()
	theme := ui.DefaultTheme()
	headerHeight := 2
	keybarHeight := 1
	bodyHeight := height - headerHeight - keybarHeight
	if bodyHeight < 8 {
		bodyHeight = 8
	}
	leftWidth := width * 52 / 100
	if leftWidth < 38 {
		leftWidth = 38
	}
	if leftWidth > 62 {
		leftWidth = 62
	}
	rightWidth := width - leftWidth - 1
	if rightWidth < 28 {
		rightWidth = 28
		leftWidth = width - rightWidth - 1
	}
	header := m.renderHeader(theme, width, headerHeight)
	left := theme.Panel.Width(leftWidth - 2).Height(bodyHeight - 2).Render(m.renderList(theme, leftWidth-4, bodyHeight-2))
	right := theme.Panel.Width(rightWidth - 2).Height(bodyHeight - 2).Render(m.renderDetails(theme, rightWidth-4, bodyHeight-2))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	keybar := m.renderKeybar(theme, width)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, keybar)
}

func (m Model) dimensions() (int, int) {
	width := m.Width
	if width <= 0 {
		width = 100
	}
	height := m.Height
	if height <= 0 {
		height = 30
	}
	if width < 70 {
		width = 70
	}
	return width, height
}

func (m Model) renderHeader(theme ui.Theme, width, height int) string {
	mode := "findings"
	if m.Mode == ViewSkills {
		mode = "skills"
	}
	title := theme.AppTitle.Render("unlearn") + theme.Muted.Render("  cleanup workbench")
	stats := []string{
		theme.Badge.Render(fmt.Sprintf("%d skills", len(m.Skills))),
		theme.BadgeWarn.Render(fmt.Sprintf("%d findings", len(m.Findings))),
		theme.Badge.Render(mode),
	}
	if m.Status != "" {
		stats = append(stats, theme.Status.Render(ui.Truncate(m.Status, max(10, width/4))))
	}
	line := padBetween(title, lipgloss.JoinHorizontal(lipgloss.Center, stats...), width)
	sep := theme.Muted.Render(strings.Repeat("─", max(0, width)))
	return strings.Join(ui.PadLines([]string{ui.Truncate(line, width), sep}, height), "\n")
}

func (m Model) renderList(theme ui.Theme, width, height int) string {
	lines := []string{theme.PanelTitle.Render(m.listTitle())}
	if m.itemCount() == 0 {
		lines = append(lines, "", theme.Muted.Render("No items yet"))
		return strings.Join(ui.PadLines(lines, height), "\n")
	}
	if m.Mode == ViewFindings {
		lines = append(lines, m.renderFindingRows(theme, width, height-1)...)
	} else {
		lines = append(lines, m.renderSkillRows(theme, width, height-1)...)
	}
	return strings.Join(ui.PadLines(ui.FitLines(lines, height), height), "\n")
}

func (m Model) listTitle() string {
	if m.Mode == ViewSkills {
		return "Skill inventory"
	}
	return "Findings"
}

func (m Model) renderFindingRows(theme ui.Theme, width, height int) []string {
	sections := groupedFindings(m.Findings)
	selected := 0
	selectedLine := 0
	var lines []string
	for _, section := range sections {
		sectionLine := fmt.Sprintf("▾ %s", section.Title)
		countLine := fmt.Sprintf("%d skills", sectionSkillCount(section))
		lines = append(lines, theme.Section.Render(ui.Truncate(padBetween(sectionLine, countLine, width), width)))
		for _, finding := range section.Findings {
			prefix := "  "
			if selected == m.Cursor {
				prefix = "▸ "
				selectedLine = len(lines)
			}
			line := prefix + findingRowText(finding, width-2)
			if selected == m.Cursor {
				line = theme.SelectedRow.Width(width).Render(ui.Truncate(line, width))
			} else {
				line = theme.Row.Render(ui.Truncate(line, width))
			}
			lines = append(lines, line)
			selected++
		}
	}
	return windowLines(lines, height, selectedLine)
}

func findingRowText(finding analysis.Finding, width int) string {
	nameWidth := width - 24
	if nameWidth < 12 {
		nameWidth = 12
	}
	meta := findingInstallLabel(finding)
	if finding.Type == analysis.FindingHighTokenCost {
		meta += " · " + tokenRange(finding.Skills)
	}
	if finding.Type == analysis.FindingBroadActivation {
		meta += " · high risk"
	}
	if finding.Type == analysis.FindingOverlap {
		meta += " · cluster"
	}
	return padBetween(ui.Truncate(finding.Title, nameWidth), meta, width)
}

func (m Model) renderSkillRows(theme ui.Theme, width, height int) []string {
	var lines []string
	for i, group := range m.SkillGroups {
		prefix := "  "
		if i == m.Cursor {
			prefix = "▸ "
		}
		kind := string(group.Representative.Kind)
		if kind == "" {
			kind = "skill"
		}
		meta := fmt.Sprintf("%s · %s", installLabel(len(group.Skills)), tokenRange(group.Skills))
		if len(group.Skills) == 1 {
			meta = fmt.Sprintf("%s · %s", kind, tokenRange(group.Skills))
		}
		line := prefix + padBetween(ui.Truncate(group.Name, width-24), meta, width-2)
		if i == m.Cursor {
			line = theme.SelectedRow.Width(width).Render(ui.Truncate(line, width))
		} else {
			line = theme.Row.Render(ui.Truncate(line, width))
		}
		lines = append(lines, line)
	}
	return windowLines(lines, height, m.Cursor)
}

func (m Model) renderDetails(theme ui.Theme, width, height int) string {
	lines := []string{theme.PanelTitle.Render("Details")}
	if m.itemCount() == 0 {
		lines = append(lines, "", theme.Muted.Render("Nothing selected"))
		return strings.Join(ui.PadLines(lines, height), "\n")
	}
	if m.State != StateNormal {
		lines = append(lines, m.renderInteraction(theme, width)...)
		return strings.Join(ui.PadLines(ui.FitLines(lines, height), height), "\n")
	}
	if m.Mode == ViewFindings {
		lines = append(lines, m.renderFindingDetails(theme, width, height-1)...)
	} else {
		lines = append(lines, m.renderSkillGroupDetails(theme, width, height-1, m.SkillGroups[m.Cursor])...)
	}
	return strings.Join(ui.PadLines(ui.FitLines(lines, height), height), "\n")
}

func (m Model) renderInteraction(theme ui.Theme, width int) []string {
	lines := []string{"", theme.BadgeWarn.Render("CONFIRM"), ""}
	for _, line := range ui.Wrap(m.Message, width) {
		lines = append(lines, theme.Row.Render(line))
	}
	if m.Input != "" || m.State == StateInputDelete || m.State == StateInputRename || m.State == StateInputRestore {
		lines = append(lines, "", theme.Accent.Render("› ")+ui.Truncate(m.Input, width-2))
	}
	return lines
}

func (m Model) renderFindingDetails(theme ui.Theme, width, height int) []string {
	finding, ok := m.selectedFinding()
	if !ok {
		return []string{"", theme.Muted.Render("Nothing selected")}
	}
	lines := []string{"", findingBadge(theme, finding.Type) + " " + theme.Accent.Render(ui.Truncate(finding.Title, width-12)), ""}
	for _, reason := range finding.Reasons {
		lines = appendBullet(lines, theme, reason, width)
	}
	lines = append(lines, "", theme.Section.Render("Summary"))
	lines = append(lines, theme.Muted.Render(ui.Truncate("• "+installLabel(len(finding.Skills))+" across "+rootSummary(finding.Skills, 2), width)))
	lines = append(lines, theme.Muted.Render(ui.Truncate("• tokens "+tokenRange(finding.Skills), width)))
	lines = append(lines, "", theme.Section.Render("Instances"))
	limit := max(1, height-len(lines)-1)
	if limit > 4 {
		limit = 4
	}
	for i, skill := range finding.Skills {
		if i >= limit {
			lines = append(lines, theme.Muted.Render(ui.Truncate(fmt.Sprintf("… %d more installs", len(finding.Skills)-i), width)))
			break
		}
		root := strings.TrimPrefix(skill.Root, homePrefix())
		meta := fmt.Sprintf("%s · %s tokens · %s", root, tokenRange([]inventory.Skill{skill}), riskLabel(skill.ActivationRisk))
		lines = append(lines, theme.Row.Render(ui.Truncate("▸ "+skill.Name, width)))
		lines = append(lines, theme.Muted.Render(ui.Truncate("  "+meta, width)))
	}
	return lines
}

func (m Model) renderSkillGroupDetails(theme ui.Theme, width, height int, group skillGroup) []string {
	skill := group.Representative
	lines := []string{"", theme.Accent.Render(ui.Truncate(group.Name, width)) + " " + theme.Badge.Render(installLabel(len(group.Skills))), ""}
	if skill.Description != "" {
		for _, line := range ui.Wrap(skill.Description, width) {
			lines = append(lines, theme.Row.Render(line))
		}
		lines = append(lines, "")
	}
	facts := []string{
		"tokens " + tokenRange(group.Skills),
		"activation " + riskLabel(skill.ActivationRisk),
		"kind " + kindLabel(skill.Kind),
		"roots " + rootSummary(group.Skills, 2),
	}
	for _, fact := range facts {
		lines = append(lines, theme.Muted.Render(ui.Truncate("• "+fact, width)))
	}
	lines = append(lines, "", theme.Section.Render("Installs"))
	limit := max(1, height-len(lines))
	if limit > 4 {
		limit = 4
	}
	for i, item := range group.Skills {
		if i >= limit {
			lines = append(lines, theme.Muted.Render(ui.Truncate(fmt.Sprintf("… %d more installs", len(group.Skills)-i), width)))
			break
		}
		root := strings.TrimPrefix(item.Root, homePrefix())
		lines = append(lines, theme.Muted.Render(ui.Truncate("• "+root, width)))
	}
	if skill.Provenance != "" && len(lines) < height-2 {
		lines = append(lines, "", theme.Section.Render("Provenance"))
		for _, line := range ui.Wrap(skill.Provenance, width) {
			lines = append(lines, theme.Row.Render(line))
		}
	}
	return lines
}

func findingBadge(theme ui.Theme, typ analysis.FindingType) string {
	label := findingTypeBadge(typ)
	switch typ {
	case analysis.FindingConflict, analysis.FindingBroken:
		return theme.BadgeDanger.Render(label)
	case analysis.FindingHighTokenCost, analysis.FindingBroadActivation:
		return theme.BadgeWarn.Render(label)
	case analysis.FindingDuplicate:
		return theme.BadgeSuccess.Render(label)
	default:
		return theme.Badge.Render(label)
	}
}

func (m Model) renderKeybar(theme ui.Theme, width int) string {
	parts := m.keyParts()
	limit := width - 2
	var out []string
	used := 0
	hidden := false
	for _, part := range parts {
		rendered := theme.Key.Render(part.Key) + " " + part.Label
		separator := "  "
		if len(out) == 0 {
			separator = ""
		}
		partWidth := lipgloss.Width(separator + rendered)
		if used+partWidth > limit {
			hidden = true
			break
		}
		out = append(out, separator+rendered)
		used += partWidth
	}
	line := strings.Join(out, "")
	if hidden {
		ellipsis := theme.Muted.Render("  …")
		for lipgloss.Width(line+ellipsis) > limit && len(out) > 1 {
			out = out[:len(out)-1]
			line = strings.Join(out, "")
		}
		line += ellipsis
	}
	return theme.Keybar.Width(limit).Render(ui.Truncate(line, limit))
}

type keyPart struct{ Key, Label string }

func (m Model) keyParts() []keyPart {
	if m.State != StateNormal {
		switch m.State {
		case StateWriteGate, StateConfirmQuarantine, StatePreviewRename:
			return []keyPart{{"y", "confirm"}, {"n", "cancel"}, {"esc", "back"}}
		case StateInputDelete, StateInputRename, StateInputRestore:
			return []keyPart{{"type", "input"}, {"enter", "submit"}, {"esc", "cancel"}}
		}
	}
	parts := []keyPart{{"↑↓/jk", "move"}, {"r", "density"}}
	if m.Mode == ViewFindings {
		parts = append(parts, keyPart{"s", "skills"}, keyPart{"I", "ignore"})
	} else {
		parts = append(parts, keyPart{"f", "findings"})
	}
	parts = append(parts, keyPart{"K", "keep"}, keyPart{"Q", "quarantine"}, keyPart{"D", "delete"}, keyPart{"N", "rename"}, keyPart{"R", "restore"}, keyPart{"q", "quit"})
	return parts
}

func (m Model) itemCount() int {
	if m.Mode == ViewFindings {
		return len(m.Findings)
	}
	return len(m.SkillGroups)
}

func windowLines(lines []string, height int, selectedLine int) []string {
	if height <= 0 || len(lines) <= height {
		return ui.FitLines(lines, height)
	}
	start := selectedLine - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	out := append([]string(nil), lines[start:start+height]...)
	if start > 0 {
		out[0] = ui.Truncate("… above", lipgloss.Width(out[0]))
	}
	if start+height < len(lines) {
		out[len(out)-1] = ui.Truncate("… more", lipgloss.Width(out[len(out)-1]))
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

func appendBullet(lines []string, theme ui.Theme, value string, width int) []string {
	wrapped := ui.Wrap(value, width-2)
	for i, line := range wrapped {
		prefix := "  "
		if i == 0 {
			prefix = "• "
		}
		lines = append(lines, theme.Muted.Render(ui.Truncate(prefix+line, width)))
	}
	return lines
}

func riskLabel(risk string) string {
	if risk == "" {
		return "unknown"
	}
	return risk
}

func kindLabel(kind inventory.SkillKind) string {
	if kind == "" {
		return "skill"
	}
	return string(kind)
}

func rootSummary(skills []inventory.Skill, limit int) string {
	if len(skills) == 0 {
		return "none"
	}
	seen := map[string]bool{}
	var roots []string
	for _, skill := range skills {
		root := strings.TrimPrefix(skill.Root, homePrefix())
		if root == "" {
			root = "unknown"
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	if limit > 0 && len(roots) > limit {
		return strings.Join(roots[:limit], ", ") + fmt.Sprintf(" +%d", len(roots)-limit)
	}
	return strings.Join(roots, ", ")
}

func homePrefix() string { return "" }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
