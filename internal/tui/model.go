package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
	StateConfirmDelete
	StateInputRename
	StatePreviewRename
	StateSelectRestore
	StateSelectInstall
	StateSelectBatchRoot
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
	Skills       []inventory.Skill
	SkillGroups  []skillGroup
	Findings     []analysis.Finding
	Actions      ActionService
	Mode         ViewMode
	Density      Density
	Cursor       int
	DetailCursor int
	Width        int
	Height       int

	State             InteractionState
	PendingAction     PendingAction
	PendingSkill      inventory.Skill
	PendingSkills     []inventory.Skill
	PendingFinding    analysis.Finding
	InstallCursor     int
	InstallSelections map[int]bool
	RestoreCursor     int
	RestoreChoices    []string
	BatchRootCursor   int
	BatchRootChoices  []fsactions.BatchRootChoice
	Input             string
	Message           string
	Status            string
	RenamePreview     fsactions.RenamePreview
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
			m.DetailCursor = 0
		}
	case "k", "up":
		if m.Cursor > 0 {
			m.Cursor--
			m.DetailCursor = 0
		}
	case "tab":
		m.moveDetailCursor(1)
	case "shift+tab":
		m.moveDetailCursor(-1)
	case "s":
		m.Mode = ViewSkills
		m.Cursor = 0
		m.DetailCursor = 0
	case "f":
		m.Mode = ViewFindings
		m.Cursor = 0
		m.DetailCursor = 0
	case "r":
		if m.Density == DensityCompact {
			m.Density = DensityRich
		} else {
			m.Density = DensityCompact
		}
	case "ctrl+k":
		m.keepSelected()
	case "ctrl+g":
		m.ignoreSelectedFinding()
	case "ctrl+q":
		m.beginSkillAction(ActionQuarantine)
	case "ctrl+d":
		m.beginSkillAction(ActionDelete)
	case "ctrl+r":
		m.beginSkillAction(ActionRename)
	case "ctrl+u":
		m.beginSkillAction(ActionRestore)
	case "ctrl+b":
		m.beginBatchRootAction()
	}
	return m, nil
}

func (m Model) updateInteraction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.State {
	case StateWriteGate:
		return m.updateWriteGate(msg)
	case StateConfirmQuarantine:
		return m.updateQuarantineConfirm(msg)
	case StateConfirmDelete:
		return m.updateDeleteConfirm(msg)
	case StateInputRename:
		return m.updateInput(msg)
	case StateSelectRestore:
		return m.updateRestoreSelection(msg)
	case StatePreviewRename:
		return m.updateRenamePreview(msg)
	case StateSelectInstall:
		return m.updateInstallSelection(msg)
	case StateSelectBatchRoot:
		return m.updateBatchRootSelection(msg)
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
		m.continuePendingWithSelectedSkills()
	case "n", "N", "esc":
		m.cancel("write permission declined")
	}
	return m, nil
}

func (m Model) updateQuarantineConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		result, err := m.Actions.QuarantineSelected(m.selectedPendingSkills())
		if err != nil {
			m.fail(err)
			return m, nil
		}
		for _, skill := range result.Skills {
			m.removeSkillFromModel(skill)
		}
		if len(result.Skills) == 1 {
			dest := ""
			if len(result.Paths) > 0 {
				dest = result.Paths[0]
			}
			m.complete(fmt.Sprintf("quarantined %s -> %s", result.Skills[0].Name, dest))
		} else {
			m.complete(fmt.Sprintf("quarantined %d installs", len(result.Skills)))
		}
	case "n", "N", "esc":
		m.cancel("quarantine cancelled")
	}
	return m, nil
}

func (m Model) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		selected := m.selectedPendingSkills()
		result, err := m.Actions.DeleteSelected(selected, deleteConfirmationFor(selected))
		if err != nil {
			m.fail(err)
			return m, nil
		}
		for _, skill := range result.Skills {
			m.removeSkillFromModel(skill)
		}
		if len(result.Skills) == 1 {
			m.complete("deleted " + result.Skills[0].Name)
		} else {
			m.complete(fmt.Sprintf("deleted %d installs", len(result.Skills)))
		}
	case "n", "N", "esc":
		m.cancel("delete cancelled")
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

func (m Model) updateRestoreSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.RestoreCursor < len(m.RestoreChoices)-1 {
			m.RestoreCursor++
		}
	case "k", "up":
		if m.RestoreCursor > 0 {
			m.RestoreCursor--
		}
	case "enter":
		if len(m.RestoreChoices) == 0 {
			m.cancel("no quarantined skills")
			return m, nil
		}
		dest, err := m.Actions.Restore(m.RestoreChoices[m.RestoreCursor], m.PendingSkill.Root)
		if err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("restored %s -> %s", m.RestoreChoices[m.RestoreCursor], dest))
		}
	case "esc", "q":
		m.cancel("restore cancelled")
	}
	return m, nil
}

func (m Model) updateInstallSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	skills := m.pendingInstallChoices()
	maxCursor := len(skills) - 1
	if m.canActOnAllInstalls() {
		maxCursor = len(skills)
	}
	switch msg.String() {
	case "j", "down":
		if m.InstallCursor < maxCursor {
			m.InstallCursor++
		}
	case "k", "up":
		if m.InstallCursor > 0 {
			m.InstallCursor--
		}
	case " ":
		if m.InstallCursor < len(skills) {
			if m.InstallSelections == nil {
				m.InstallSelections = map[int]bool{}
			}
			m.InstallSelections[m.InstallCursor] = !m.InstallSelections[m.InstallCursor]
		}
	case "enter":
		selected, err := fsactions.ResolveSelection(fsactions.SelectionInput{
			Kind:     destructiveKind(m.PendingAction),
			Choices:  skills,
			Cursor:   m.InstallCursor,
			Marked:   m.InstallSelections,
			AllowAll: true,
		})
		if err != nil {
			m.cancel(err.Error())
			return m, nil
		}
		m.PendingSkills = selected
		m.PendingSkill = selected[0]
		m.continuePendingWithSelectedSkills()
	case "esc", "q":
		m.cancel("action cancelled")
	}
	return m, nil
}

func (m Model) updateBatchRootSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.BatchRootCursor < len(m.BatchRootChoices)-1 {
			m.BatchRootCursor++
		}
	case "k", "up":
		if m.BatchRootCursor > 0 {
			m.BatchRootCursor--
		}
	case "enter":
		if len(m.BatchRootChoices) == 0 {
			m.cancel("no duplicate root selected")
			return m, nil
		}
		choice := m.BatchRootChoices[m.BatchRootCursor]
		m.PendingAction = ActionQuarantine
		m.PendingSkills = append([]inventory.Skill(nil), choice.Skills...)
		m.PendingSkill = choice.Skills[0]
		m.Message = fmt.Sprintf("Quarantine duplicate installs from %s", choice.Root)
		m.continuePendingWithSelectedSkills()
	case "esc", "q":
		m.cancel("batch cleanup cancelled")
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
	m.PendingAction = action
	if action == ActionRestore {
		m.beginRestoreAction()
		return
	}
	if action != ActionRestore {
		if finding, ok := m.selectedFinding(); ok && len(finding.Skills) > 1 {
			m.PendingFinding = finding
			m.PendingSkill = inventory.Skill{}
			m.InstallCursor = m.clampedDetailCursor(finding)
			m.State = StateSelectInstall
			m.Message = fmt.Sprintf("Choose the exact %s install to %s", finding.Title, actionVerb(action))
			return
		}
		if group, ok := m.selectedSkillGroup(); ok && len(group.Skills) > 1 {
			m.PendingFinding = analysis.Finding{Title: group.Name, Skills: group.Skills}
			m.PendingSkill = inventory.Skill{}
			m.InstallCursor = 0
			m.State = StateSelectInstall
			m.Message = fmt.Sprintf("Choose the exact %s install to %s", group.Name, actionVerb(action))
			return
		}
	}
	skill, ok := m.selectedSkill()
	if !ok {
		m.Status = "no skill selected"
		return
	}
	m.PendingSkill = skill
	m.PendingSkills = []inventory.Skill{skill}
	m.continuePendingWithSelectedSkills()
}

func (m *Model) continuePendingWithSelectedSkills() {
	if skill, ok := m.Actions.FirstMissingWrite(m.selectedPendingSkills()); ok {
		m.PendingSkill = skill
		m.State = StateWriteGate
		m.Message = fmt.Sprintf("Allow write access for this install?\n%s", skillTarget(skill))
		return
	}
	m.continuePendingAfterWriteGate()
}

func (m *Model) continuePendingAfterWriteGate() {
	target := m.pendingTargetText()
	scope := "this exact install"
	if len(m.selectedPendingSkills()) > 1 {
		scope = fmt.Sprintf("all %d installs", len(m.selectedPendingSkills()))
	}
	switch m.PendingAction {
	case ActionQuarantine:
		m.State = StateConfirmQuarantine
		m.Message = fmt.Sprintf("Move %s into unlearn quarantine?\n%s", scope, target)
	case ActionDelete:
		m.State = StateConfirmDelete
		m.Message = fmt.Sprintf("Permanently delete %s?\n%s", scope, target)
	case ActionRename:
		m.State = StateInputRename
		m.Input = ""
		m.Message = fmt.Sprintf("Rename this exact install?\n%s\nNew name", skillTarget(m.PendingSkill))
	default:
		m.resetInteraction()
	}
}

func (m *Model) submitInput() {
	switch m.State {
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
	}
}

func (m *Model) beginBatchRootAction() {
	choices := m.duplicateRootChoices()
	if len(choices) == 0 {
		m.Status = "no duplicate roots to batch clean"
		return
	}
	m.PendingAction = ActionQuarantine
	m.BatchRootChoices = choices
	m.BatchRootCursor = 0
	m.State = StateSelectBatchRoot
	m.Message = "Choose a root to quarantine duplicate installs from"
}

func (m *Model) beginRestoreAction() {
	skill, ok := m.selectedSkill()
	if !ok {
		m.Status = "select a destination skill/root before restore"
		return
	}
	choices, err := m.Actions.QuarantinedSkills()
	if err != nil {
		m.fail(err)
		return
	}
	m.PendingSkill = skill
	m.RestoreChoices = choices
	m.RestoreCursor = 0
	m.State = StateSelectRestore
	m.Message = fmt.Sprintf("Restore quarantined skill into %s", skill.Root)
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
		group, ok := m.selectedSkillGroup()
		if !ok {
			return inventory.Skill{}, false
		}
		return group.Representative, true
	}
	finding, ok := m.selectedFinding()
	if !ok || len(finding.Skills) == 0 {
		return inventory.Skill{}, false
	}
	return finding.Skills[m.clampedDetailCursor(finding)], true
}

func (m *Model) moveDetailCursor(delta int) {
	if m.Mode != ViewFindings {
		return
	}
	finding, ok := m.selectedFinding()
	if !ok || len(finding.Skills) == 0 {
		m.DetailCursor = 0
		return
	}
	m.DetailCursor += delta
	if m.DetailCursor < 0 {
		m.DetailCursor = len(finding.Skills) - 1
	}
	if m.DetailCursor >= len(finding.Skills) {
		m.DetailCursor = 0
	}
}

func (m Model) clampedDetailCursor(finding analysis.Finding) int {
	if len(finding.Skills) == 0 || m.DetailCursor < 0 {
		return 0
	}
	if m.DetailCursor >= len(finding.Skills) {
		return len(finding.Skills) - 1
	}
	return m.DetailCursor
}

func (m Model) selectedSkillGroup() (skillGroup, bool) {
	if m.Mode != ViewSkills || len(m.SkillGroups) == 0 || m.Cursor < 0 || m.Cursor >= len(m.SkillGroups) {
		return skillGroup{}, false
	}
	return m.SkillGroups[m.Cursor], true
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

func (m *Model) removeSkillFromModel(removed inventory.Skill) {
	m.Skills = removeSkill(m.Skills, removed)
	m.SkillGroups = groupedSkills(m.Skills)
	m.Findings = pruneFindings(m.Findings, removed)
	if m.Cursor >= m.itemCount() {
		m.Cursor = max(0, m.itemCount()-1)
	}
}

func removeSkill(skills []inventory.Skill, removed inventory.Skill) []inventory.Skill {
	out := make([]inventory.Skill, 0, len(skills))
	for _, skill := range skills {
		if !sameSkillInstall(skill, removed) {
			out = append(out, skill)
		}
	}
	return out
}

func pruneFindings(findings []analysis.Finding, removed inventory.Skill) []analysis.Finding {
	out := make([]analysis.Finding, 0, len(findings))
	for _, finding := range findings {
		finding.Skills = removeSkill(finding.Skills, removed)
		if keepFindingAfterRemoval(finding) {
			out = append(out, finding)
		}
	}
	return out
}

func keepFindingAfterRemoval(finding analysis.Finding) bool {
	switch finding.Type {
	case analysis.FindingDuplicate, analysis.FindingConflict, analysis.FindingOverlap:
		return len(finding.Skills) > 1
	default:
		return len(finding.Skills) > 0
	}
}

func sameSkillInstall(a, b inventory.Skill) bool {
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	if a.EncounteredPath != "" && b.EncounteredPath != "" && a.EncounteredPath == b.EncounteredPath {
		return true
	}
	if a.PrimaryPath != "" && b.PrimaryPath != "" && a.PrimaryPath == b.PrimaryPath {
		return true
	}
	if !strings.EqualFold(a.Name, b.Name) || a.Root != b.Root {
		return false
	}
	aPath := firstNonEmpty(a.EncounteredPath, a.PrimaryPath)
	bPath := firstNonEmpty(b.EncounteredPath, b.PrimaryPath)
	return aPath == "" || bPath == "" || aPath == bPath
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (m *Model) resetInteraction() {
	m.State = StateNormal
	m.PendingAction = ActionNone
	m.PendingSkill = inventory.Skill{}
	m.PendingSkills = nil
	m.PendingFinding = analysis.Finding{}
	m.InstallCursor = 0
	m.InstallSelections = nil
	m.RestoreCursor = 0
	m.BatchRootCursor = 0
	m.BatchRootChoices = nil
	m.RestoreChoices = nil
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
	body := ""
	if m.State != StateNormal {
		body = m.renderModalBody(theme, width, bodyHeight)
	} else {
		left := theme.Panel.Width(leftWidth - 2).Height(bodyHeight - 2).Render(m.renderList(theme, leftWidth-4, bodyHeight-2))
		right := theme.Panel.Width(rightWidth - 2).Height(bodyHeight - 2).Render(m.renderDetails(theme, rightWidth-4, bodyHeight-2))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
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
	density := "compact"
	if m.Density == DensityRich {
		density = "rich"
	}
	stats := []string{
		theme.Badge.Render(fmt.Sprintf("%d skills", len(m.Skills))),
		theme.BadgeWarn.Render(fmt.Sprintf("%d findings", len(m.Findings))),
		theme.Badge.Render(mode),
		theme.Badge.Render(density),
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
	if m.Density == DensityRich {
		return "Findings · rich"
	}
	return "Findings"
}

func (m Model) renderFindingRows(theme ui.Theme, width, height int) []string {
	sections := groupedFindings(m.Findings)
	selected := 0
	selectedLine := 0
	var lines []string
	for _, section := range sections {
		lines = append(lines, renderFindingSectionHeader(theme, section, width))
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

func renderFindingSectionHeader(theme ui.Theme, section findingSection, width int) string {
	title := theme.Accent.Render("▌ ") + theme.Section.Render(section.Title)
	count := theme.Muted.Render(fmt.Sprintf("%d skills", sectionSkillCount(section)))
	line := padBetween(title, count, width)
	return ui.Truncate(line, width)
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
	if hasLLMReason(finding) {
		meta += " · LLM"
	}
	return padBetween(ui.Truncate(finding.Title, nameWidth), meta, width)
}

func (m Model) renderSkillRows(theme ui.Theme, width, height int) []string {
	var lines []string
	selectedLine := 0
	for i, group := range m.SkillGroups {
		prefix := "  "
		if i == m.Cursor {
			prefix = "▸ "
			selectedLine = len(lines)
		}
		meta := fmt.Sprintf("%s · %s", installLabel(len(group.Skills)), tokenRange(group.Skills))
		if len(group.Skills) == 1 {
			meta = tokenRange(group.Skills)
		}
		line := prefix + padBetween(ui.Truncate(group.Name, width-24), meta, width-2)
		if i == m.Cursor {
			line = theme.SelectedRow.Width(width).Render(ui.Truncate(line, width))
		} else {
			line = theme.Row.Render(ui.Truncate(line, width))
		}
		lines = append(lines, line)
	}
	return windowLines(lines, height, selectedLine)
}

func (m Model) renderModalBody(theme ui.Theme, width, height int) string {
	modalWidth := width - 16
	if modalWidth > 76 {
		modalWidth = 76
	}
	if modalWidth < 52 {
		modalWidth = width - 4
	}
	contentWidth := modalWidth - 6
	lines := m.renderInteraction(theme, contentWidth)
	modal := theme.Modal.Width(modalWidth - 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
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
	label := "CONFIRM ACTION"
	if m.State == StateSelectInstall {
		label = "CHOOSE EXACT INSTALL"
	}
	if m.State == StateSelectRestore {
		label = "RESTORE SKILL"
	}
	if m.State == StateSelectBatchRoot {
		label = "BATCH DUPLICATES BY ROOT"
	}
	lines := []string{theme.BadgeWarn.Render(label), ""}
	messageLines := strings.Split(m.Message, "\n")
	if len(messageLines) > 0 && strings.TrimSpace(messageLines[0]) != "" {
		for _, line := range ui.Wrap(messageLines[0], width) {
			lines = append(lines, theme.Section.Render(line))
		}
	}
	if len(messageLines) > 1 {
		lines = append(lines, "", theme.Muted.Render("Target"))
		for _, target := range messageLines[1:] {
			for _, line := range ui.Wrap(target, width-2) {
				lines = append(lines, theme.Row.Render("  "+line))
			}
		}
	}
	if m.State == StateSelectRestore {
		lines = append(lines, "", theme.Muted.Render("Use ↑/↓ then enter:"))
		if len(m.RestoreChoices) == 0 {
			lines = append(lines, theme.Muted.Render("  No quarantined skills found"))
		}
		for i, name := range m.RestoreChoices {
			prefix := "  "
			style := theme.Row
			if i == m.RestoreCursor {
				prefix = "▸ "
				style = theme.SelectedRow.Width(width)
			}
			lines = append(lines, style.Render(ui.Truncate(prefix+name, width)))
		}
	}
	if m.State == StateSelectBatchRoot {
		lines = append(lines, "", theme.Muted.Render("Use ↑/↓ then enter:"))
		for i, choice := range m.BatchRootChoices {
			prefix := "  "
			style := theme.Row
			if i == m.BatchRootCursor {
				prefix = "▸ "
				style = theme.SelectedRow.Width(width)
			}
			line := fmt.Sprintf("%s · %d duplicate installs", choice.Root, len(choice.Skills))
			lines = append(lines, style.Render(ui.Truncate(prefix+line, width)))
		}
	}
	if m.State == StateSelectInstall {
		lines = append(lines, "", theme.Muted.Render("Use ↑/↓, space to mark many, enter:"))
		for i, skill := range m.pendingInstallChoices() {
			mark := "[ ]"
			if m.InstallSelections[i] {
				mark = "[x]"
			}
			prefix := "  "
			style := theme.Row
			if i == m.InstallCursor {
				prefix = "▸ "
				style = theme.SelectedRow.Width(width)
			}
			lines = append(lines, style.Render(ui.Truncate(prefix+mark+" "+installChoiceLabel(skill), width)))
		}
		if m.canActOnAllInstalls() {
			prefix := "  "
			style := theme.Row
			if m.InstallCursor == len(m.pendingInstallChoices()) {
				prefix = "▸ "
				style = theme.SelectedRow.Width(width)
			}
			lines = append(lines, style.Render(ui.Truncate(prefix+fmt.Sprintf("All %d installs", len(m.pendingInstallChoices())), width)))
		}
	}
	if m.Input != "" || m.State == StateInputRename {
		lines = append(lines, "", theme.Muted.Render("Input"), theme.Accent.Render("› ")+ui.Truncate(m.Input, width-2))
	}
	lines = append(lines, "", theme.Muted.Render("Options"), optionLineForState(theme, m.State))
	return lines
}

func (m Model) renderFindingDetails(theme ui.Theme, width, height int) []string {
	finding, ok := m.selectedFinding()
	if !ok {
		return []string{"", theme.Muted.Render("Nothing selected")}
	}
	selected := m.clampedDetailCursor(finding)
	header := findingBadge(theme, finding.Type)
	if hasLLMReason(finding) {
		header += " " + theme.Badge.Render("LLM")
	}
	lines := []string{"", header + " " + theme.Accent.Render(ui.Truncate(finding.Title, width-12)), ""}
	for _, reason := range finding.Reasons {
		lines = appendBullet(lines, theme, reason, width)
	}
	lines = append(lines, "", theme.Section.Render("Summary"))
	lines = append(lines, theme.Muted.Render(ui.Truncate("• "+installLabel(len(finding.Skills))+" across "+rootSummary(finding.Skills, 2), width)))
	if finding.Type != analysis.FindingOverlap {
		lines = append(lines, theme.Muted.Render(ui.Truncate("• tokens "+tokenRange(finding.Skills), width)))
	}
	if summary := findingHistoryEvidenceSummary(finding); summary != "" {
		lines = append(lines, theme.Muted.Render(ui.Truncate("• history "+summary, width)))
	}
	lines = append(lines, "", theme.Section.Render("Compare installs"))
	for i, skill := range finding.Skills {
		prefix := "  "
		style := theme.Row
		if i == selected {
			prefix = "▸ "
			style = theme.SelectedRow.Width(width)
		}
		root := strings.TrimPrefix(skill.Root, homePrefix())
		parts := []string{root, tokenRange([]inventory.Skill{skill}), riskLabel(skill.ActivationRisk)}
		if finding.Type == analysis.FindingOverlap {
			parts = append([]string{skill.Name}, parts...)
		}
		meta := strings.Join(parts, " · ")
		desc := descriptionSnippet(skill, 32)
		if desc != "" && !(i == selected && m.Density == DensityRich) {
			meta += " · " + desc
		}
		lines = append(lines, style.Render(ui.Truncate(prefix+meta, width)))
		if i == selected && m.Density == DensityRich {
			lines = append(lines, renderSelectedInstallDetails(theme, skill, width)...)
		}
	}
	lines = append(lines, "")
	lines = append(lines, renderDetailShortcuts(theme, m.Density, len(finding.Skills) > 1, width)...)
	return lines
}

func (m Model) renderSkillGroupDetails(theme ui.Theme, width, height int, group skillGroup) []string {
	skill := group.Representative
	lines := []string{"", theme.Accent.Render(ui.Truncate(group.Name, width)) + " " + theme.Badge.Render(installLabel(len(group.Skills))), ""}
	description := skill.Description
	if broadGenericDescription(description) {
		description = "Description is broad and not distinctive; review the exact install paths below before acting."
	}
	if description != "" {
		for _, line := range ui.Wrap(description, width) {
			lines = append(lines, theme.Row.Render(line))
		}
		lines = append(lines, "")
	}
	if summary, provider, model := llmSummaryForGroup(group); summary != "" {
		label := llmSummaryDetailLabel(provider, model)
		lines = append(lines, theme.Section.Render(label))
		for _, line := range ui.Wrap(summary, width) {
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
	if summary := historyEvidenceSummary(group.Skills); summary != "" {
		facts = append(facts, "history "+summary)
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

func hasLLMReason(finding analysis.Finding) bool {
	for _, reason := range finding.Reasons {
		if strings.Contains(strings.ToLower(reason), "llm-assisted") || strings.Contains(strings.ToLower(reason), "gemini/") {
			return true
		}
	}
	return false
}

func llmSummaryForGroup(group skillGroup) (string, string, string) {
	for _, skill := range append([]inventory.Skill{group.Representative}, group.Skills...) {
		if strings.TrimSpace(skill.LLMSummary) != "" && !disabledLLMSummary(skill.LLMProvider, skill.LLMModel) {
			return skill.LLMSummary, skill.LLMProvider, skill.LLMModel
		}
	}
	return "", "", ""
}

func llmSummaryDetailLabel(provider, model string) string {
	label := "Agent summary"
	if provider != "" || model != "" {
		label += fmt.Sprintf(" (%s/%s)", emptyDetailLabel(provider), emptyDetailLabel(model))
	}
	return label
}

func disabledLLMSummary(provider, model string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "disabled") && strings.EqualFold(strings.TrimSpace(model), "disabled")
}

func emptyDetailLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
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
		case StateWriteGate, StateConfirmQuarantine, StateConfirmDelete, StatePreviewRename:
			return []keyPart{{"y", "confirm"}, {"n", "cancel"}, {"esc", "back"}}
		case StateSelectInstall:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "select"}, {"esc", "cancel"}}
		case StateSelectRestore:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "restore"}, {"esc", "cancel"}}
		case StateSelectBatchRoot:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "preview"}, {"esc", "cancel"}}
		case StateInputRename:
			return []keyPart{{"type", "input"}, {"enter", "submit"}, {"esc", "cancel"}}
		}
	}
	parts := []keyPart{{"↑↓/jk", "move"}}
	if m.Mode == ViewFindings {
		parts = append(parts, keyPart{"s", "skills"}, keyPart{"ctrl+g", "ignore"})
	} else {
		parts = append(parts, keyPart{"f", "findings"})
	}
	parts = append(parts, keyPart{"ctrl+k", "keep"}, keyPart{"ctrl+q", "quarantine"}, keyPart{"ctrl+d", "delete"}, keyPart{"ctrl+r", "rename"}, keyPart{"ctrl+u", "restore"}, keyPart{"ctrl+b", "batch"}, keyPart{"q", "quit"})
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

func (m Model) duplicateRootChoices() []fsactions.BatchRootChoice {
	return fsactions.DuplicateRootChoices(m.Findings)
}

func (m Model) selectedPendingSkills() []inventory.Skill {
	if len(m.PendingSkills) > 0 {
		return m.PendingSkills
	}
	if m.PendingSkill.Name != "" {
		return []inventory.Skill{m.PendingSkill}
	}
	return nil
}

func (m Model) pendingTargetText() string {
	skills := m.selectedPendingSkills()
	if len(skills) == 1 {
		return skillTarget(skills[0])
	}
	var lines []string
	for _, skill := range skills {
		lines = append(lines, installChoiceLabel(skill))
	}
	return strings.Join(lines, "\n")
}

func (m Model) canActOnAllInstalls() bool {
	return fsactions.AllowsAllInstalls(destructiveKind(m.PendingAction), m.pendingInstallChoices())
}

func deleteConfirmationFor(skills []inventory.Skill) fsactions.DeleteConfirmation {
	if len(skills) == 1 {
		return fsactions.DeleteConfirmation{TypedName: skills[0].Name}
	}
	return fsactions.DeleteConfirmation{BatchToken: fsactions.BatchDeleteConfirmation(skills)}
}

func destructiveKind(action PendingAction) fsactions.DestructiveKind {
	switch action {
	case ActionQuarantine:
		return fsactions.DestructiveQuarantine
	case ActionDelete:
		return fsactions.DestructiveDelete
	case ActionRename:
		return fsactions.DestructiveRename
	default:
		return fsactions.DestructiveUnknown
	}
}

func optionLineForState(theme ui.Theme, state InteractionState) string {
	switch state {
	case StateWriteGate, StateConfirmQuarantine, StateConfirmDelete, StatePreviewRename:
		return theme.Key.Render("y") + " confirm  " + theme.Key.Render("n") + " cancel  " + theme.Key.Render("esc") + " back"
	case StateSelectInstall:
		return theme.Key.Render("enter") + " select highlighted install  " + theme.Key.Render("esc") + " cancel"
	case StateSelectRestore:
		return theme.Key.Render("enter") + " restore highlighted skill  " + theme.Key.Render("esc") + " cancel"
	case StateSelectBatchRoot:
		return theme.Key.Render("enter") + " preview root cleanup  " + theme.Key.Render("esc") + " cancel"
	case StateInputRename:
		return theme.Key.Render("enter") + " submit  " + theme.Key.Render("esc") + " cancel"
	default:
		return theme.Key.Render("esc") + " back"
	}
}

func renderDetailShortcuts(theme ui.Theme, density Density, hasMultipleInstalls bool, width int) []string {
	lines := []string{theme.Section.Render("Shortcuts")}
	densityHint := "r density: switch to rich details"
	if density == DensityRich {
		densityHint = "r density: switch to compact details"
	}
	lines = append(lines, theme.Muted.Render(ui.Truncate("• "+densityHint, width)))
	if hasMultipleInstalls {
		lines = append(lines, theme.Muted.Render(ui.Truncate("• tab cycle install", width)))
	}
	return lines
}

func findingHistoryEvidenceSummary(finding analysis.Finding) string {
	if finding.Type != analysis.FindingUnseen {
		return ""
	}
	summary := historyEvidenceSummary(finding.Skills)
	if summary == "" {
		return "no strong or medium invocation evidence"
	}
	return summary
}

func historyEvidenceSummary(skills []inventory.Skill) string {
	best := ""
	sources := 0
	for _, skill := range skills {
		if skill.HistoryEvidence == "weak" {
			continue
		}
		if evidenceRank(skill.HistoryEvidence) < evidenceRank(best) {
			best = skill.HistoryEvidence
		}
		sources += len(skill.HistorySources)
	}
	if best == "" {
		return ""
	}
	lastSeen := latestHistorySeen(skills)
	when := ""
	if !lastSeen.IsZero() {
		when = " · last seen " + relativeTime(lastSeen)
	}
	if sources == 0 {
		return best + " derived evidence" + when
	}
	return fmt.Sprintf("%s derived evidence from %d source(s)%s", best, sources, when)
}

func latestHistorySeen(skills []inventory.Skill) time.Time {
	var latest time.Time
	for _, skill := range skills {
		if skill.HistoryEvidence == "weak" {
			continue
		}
		if skill.HistoryLastSeenAt.After(latest) {
			latest = skill.HistoryLastSeenAt
		}
	}
	return latest
}

func relativeTime(value time.Time) string {
	duration := time.Since(value)
	if duration < 0 {
		return value.Format("2006-01-02")
	}
	switch {
	case duration < time.Hour:
		minutes := max(1, int(duration.Minutes()))
		return fmt.Sprintf("%dm ago", minutes)
	case duration < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	case duration < 45*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(duration.Hours()/24))
	default:
		return value.Format("2006-01-02")
	}
}

func evidenceRank(grade string) int {
	switch grade {
	case "strong":
		return 1
	case "medium":
		return 2
	case "weak":
		return 3
	default:
		return 99
	}
}

func renderSelectedInstallDetails(theme ui.Theme, skill inventory.Skill, width int) []string {
	var lines []string
	description := skill.Description
	if broadGenericDescription(description) {
		description = "Description is broad and not distinctive."
	}
	if description != "" {
		for _, line := range ui.Wrap(description, width-4) {
			lines = append(lines, theme.Row.Render(ui.Truncate("    "+line, width)))
		}
	}
	path := firstNonEmpty(skill.EncounteredPath, skill.PrimaryPath)
	if path != "" {
		lines = append(lines, theme.Muted.Render(ui.Truncate("    path "+path, width)))
	}
	if skill.Provenance != "" {
		lines = append(lines, theme.Muted.Render(ui.Truncate("    provenance "+skill.Provenance, width)))
	}
	if skill.HistoryEvidence != "" {
		sourceCount := len(skill.HistorySources)
		label := fmt.Sprintf("    history %s evidence", skill.HistoryEvidence)
		if sourceCount > 0 {
			label += fmt.Sprintf(" from %d source(s)", sourceCount)
		}
		if !skill.HistoryLastSeenAt.IsZero() {
			label += " · last seen " + relativeTime(skill.HistoryLastSeenAt)
		}
		lines = append(lines, theme.Muted.Render(ui.Truncate(label, width)))
	}
	return lines
}

func descriptionSnippet(skill inventory.Skill, width int) string {
	description := strings.TrimSpace(skill.Description)
	if description == "" || broadGenericDescription(description) {
		return ""
	}
	return ui.Truncate(description, width)
}

func (m Model) pendingInstallChoices() []inventory.Skill {
	if len(m.PendingFinding.Skills) > 0 {
		return m.PendingFinding.Skills
	}
	if m.PendingSkill.Name != "" {
		return []inventory.Skill{m.PendingSkill}
	}
	return nil
}

func actionVerb(action PendingAction) string {
	switch action {
	case ActionQuarantine:
		return "quarantine"
	case ActionDelete:
		return "delete"
	case ActionRename:
		return "rename"
	case ActionRestore:
		return "restore"
	default:
		return "act on"
	}
}

func skillTarget(skill inventory.Skill) string {
	parts := []string{skill.Name}
	if skill.Root != "" {
		parts = append(parts, "root "+skill.Root)
	}
	path := skill.EncounteredPath
	if path == "" {
		path = skill.PrimaryPath
	}
	if path != "" {
		parts = append(parts, "path "+path)
	}
	return strings.Join(parts, "\n")
}

func installChoiceLabel(skill inventory.Skill) string {
	path := skill.EncounteredPath
	if path == "" {
		path = skill.PrimaryPath
	}
	if path != "" {
		return fmt.Sprintf("%s · %s", skill.Name, path)
	}
	root := skill.Root
	if root == "" {
		root = "unknown root"
	}
	return fmt.Sprintf("%s · %s", skill.Name, root)
}

func broadGenericDescription(description string) bool {
	text := strings.ToLower(description)
	if strings.Contains(text, "plan build create design implement review fix improve optimize enhance refactor check") {
		return true
	}
	genericActions := []string{"plan", "build", "create", "design", "implement", "review", "fix", "improve", "optimize", "enhance", "refactor", "check"}
	matches := 0
	for _, action := range genericActions {
		if strings.Contains(text, action) {
			matches++
		}
	}
	return matches >= 8 && strings.Contains(text, "many things")
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
