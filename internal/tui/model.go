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
	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/review"
	"github.com/mblarsen/unlearn/internal/tui/picker"
	"github.com/mblarsen/unlearn/internal/ui"
	"github.com/mblarsen/unlearn/internal/workbench"
)

type ViewMode int

const (
	ViewFindings ViewMode = iota
	ViewSkills
	ViewGuidedReview
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
	StateSelectDraftSkills
	StateGeneratingDraft
	StatePreviewDraft
	StateSkillStory
	StateDiscoveryQuery
	StateDiscoveryResults
	StateDiscoveryInspect
	StateHelp
	StateFeedback
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
	Skills           []inventory.Skill
	SkillGroups      []skillGroup
	Findings         []analysis.Finding
	Actions          ActionService
	Mode             ViewMode
	Density          Density
	Cursor           int
	DetailCursor     int
	StoryScroll      int
	EvidenceCoverage audit.EvidenceCoverage
	Width            int
	Height           int

	State                       InteractionState
	PendingAction               PendingAction
	PendingSkill                inventory.Skill
	PendingSkills               []inventory.Skill
	PendingFinding              analysis.Finding
	InstallPicker               picker.Model
	RestorePicker               picker.Model
	RestoreChoices              []string
	BatchRootPicker             picker.Model
	BatchRootChoices            []fsactions.BatchRootChoice
	Input                       string
	Message                     string
	Status                      string
	StatusError                 bool
	StatusRecovery              string
	StatusContext               InteractionState
	FeedbackScroll              int
	RenamePreview               fsactions.RenamePreview
	DraftCursor                 int
	DraftScroll                 int
	DraftChoices                []draftSkillChoice
	DraftSelections             map[int]bool
	DraftPreview                string
	DraftProvider               string
	DraftModel                  string
	draftLifecycle              draftLifecycle
	Discovery                   discoveryState
	GuidedReview                review.Session
	guidedReviewDecisionPending bool
}

type draftSkillChoice struct {
	Skill          inventory.Skill
	OverlapGrouped bool
}

func New(skills []inventory.Skill, findings []analysis.Finding) Model {
	return NewWithActionsAndCoverage(skills, findings, NoopActionService{}, audit.EvidenceUnknown)
}

func NewWithActions(skills []inventory.Skill, findings []analysis.Finding, service ActionService) Model {
	return NewWithActionsAndCoverage(skills, findings, service, audit.EvidenceUnknown)
}

func NewWithActionsAndCoverage(skills []inventory.Skill, findings []analysis.Finding, service ActionService, coverage audit.EvidenceCoverage) Model {
	if service == nil {
		service = NoopActionService{}
	}
	return Model{Skills: skills, SkillGroups: groupedSkills(skills), Findings: findings, Actions: service, EvidenceCoverage: coverage, Mode: ViewFindings, Density: DensityCompact}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.State == StateSkillStory {
			maxScroll, _ := m.skillStoryScrollMetrics()
			m.StoryScroll = min(m.StoryScroll, maxScroll)
		}
	case tea.KeyMsg:
		if m.State != StateNormal {
			return m.updateInteraction(msg)
		}
		return m.updateNormal(msg)
	case draftMergeResultMsg:
		return m.handleDraftMergeResult(msg), nil
	}
	return m, nil
}

type draftMergeResultMsg struct {
	OperationID draftOperationID
	Skills      []inventory.Skill
	Result      llm.DraftResult
	Err         error
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Mode == ViewGuidedReview {
		return m.updateGuidedReview(msg)
	}
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "?":
		m.State = StateHelp
	case "x":
		m.dismissStatus()
	case "enter":
		if m.Status != "" {
			m.FeedbackScroll = 0
			m.State = StateFeedback
		} else if m.Mode == ViewSkills && m.Cursor >= 0 && m.Cursor < len(m.SkillGroups) {
			m.StoryScroll = 0
			m.State = StateSkillStory
		}
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
	case "m":
		m.beginDraftMergeAction()
	case "d":
		m.beginDiscovery()
	case "v":
		m.beginGuidedReview()
	}
	return m, nil
}

func (m Model) updateInteraction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "x" && m.Status != "" && m.StatusContext == m.State && m.State != StateInputRename {
		m.dismissStatus()
		return m, nil
	}
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
	case StateSelectDraftSkills:
		return m.updateDraftSkillSelection(msg)
	case StateGeneratingDraft:
		return m.updateDraftGeneration(msg)
	case StatePreviewDraft:
		return m.updateDraftPreview(msg)
	case StateSkillStory:
		return m.updateSkillStory(msg)
	case StateDiscoveryQuery, StateDiscoveryResults, StateDiscoveryInspect:
		return m.updateDiscovery(msg)
	case StateHelp:
		return m.updateHelp(msg)
	case StateFeedback:
		return m.updateFeedback(msg)
	default:
		m.resetInteraction()
		return m, nil
	}
}

func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc", "q":
		m.State = StateNormal
	}
	return m, nil
}

func (m Model) updateFeedback(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll, pageSize := m.feedbackScrollMetrics()
	switch msg.String() {
	case "j", "down":
		m.FeedbackScroll = min(maxScroll, m.FeedbackScroll+1)
	case "k", "up":
		m.FeedbackScroll = max(0, m.FeedbackScroll-1)
	case "pgdown", "ctrl+d":
		m.FeedbackScroll = min(maxScroll, m.FeedbackScroll+pageSize)
	case "pgup", "ctrl+u":
		m.FeedbackScroll = max(0, m.FeedbackScroll-pageSize)
	case "home", "g":
		m.FeedbackScroll = 0
	case "end", "G":
		m.FeedbackScroll = maxScroll
	case "x":
		m.dismissStatus()
	case "enter", "esc", "q":
		m.State = StateNormal
	}
	return m, nil
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
		outcome := m.Actions.Mutate(workbench.Request{Kind: workbench.Quarantine, Authorized: true, Snapshot: m.snapshot(), Targets: m.selectedPendingSkills()})
		m.applyMutationOutcome(outcome)
		if m.guidedReviewDecisionPending && (len(outcome.Removed) > 0 || len(outcome.Missing) > 0) {
			if err := m.recordGuidedReviewDecision(review.ActionQuarantine); err != nil {
				m.fail(fmt.Errorf("quarantine changed the install but review progress was not saved: %w", err))
				return m, nil
			}
		}
		status := actionResultStatus("quarantined", outcome)
		if err := outcome.Err(); err != nil {
			m.complete(actionFailureStatus(status, err))
			return m, nil
		}
		if len(outcome.Removed) == 1 && len(outcome.Paths) == 1 {
			m.complete(fmt.Sprintf("quarantined %s -> %s", outcome.Removed[0].Name, outcome.Paths[0]))
		} else {
			m.complete(status)
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
		outcome := m.Actions.Mutate(workbench.Request{Kind: workbench.Delete, Authorized: true, Snapshot: m.snapshot(), Targets: selected, Confirmation: deleteConfirmationFor(selected)})
		m.applyMutationOutcome(outcome)
		status := actionResultStatus("deleted", outcome)
		if err := outcome.Err(); err != nil {
			m.complete(actionFailureStatus(status, err))
			return m, nil
		}
		if len(outcome.Removed) == 1 && len(outcome.Missing) == 0 {
			m.complete("deleted " + outcome.Removed[0].Name)
		} else {
			m.complete(status)
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
	switch m.RestorePicker.Handle(msg.String()) {
	case picker.Submit:
		selection := m.RestorePicker.Selection()
		if !selection.HasChoice {
			m.cancel("no quarantined skills")
			return m, nil
		}
		name := m.RestoreChoices[selection.Cursor]
		outcome := m.Actions.Mutate(workbench.Request{Kind: workbench.Restore, Authorized: true, Snapshot: m.snapshot(), RestoreName: name, DestinationRoot: m.PendingSkill.Root})
		m.applyMutationOutcome(outcome)
		if err := outcome.Err(); err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("restored %s -> %s", name, outcome.Paths[0]))
		}
	case picker.Cancel:
		m.cancel("restore cancelled")
	}
	return m, nil
}

func (m Model) updateInstallSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.InstallPicker.Handle(msg.String()) {
	case picker.Cancel:
		m.cancel("action cancelled")
		return m, nil
	case picker.Submit:
		// Domain selection precedence remains in actions.ResolveSelection.
	default:
		return m, nil
	}
	selection := m.InstallPicker.Selection()
	selected, err := fsactions.ResolveSelection(fsactions.SelectionInput{
		Kind:     destructiveKind(m.PendingAction),
		Choices:  m.pendingInstallChoices(),
		Cursor:   selection.Cursor,
		Marked:   selection.Marked,
		AllowAll: true,
	})
	if err != nil {
		m.cancel(err.Error())
		return m, nil
	}
	m.PendingSkills = selected
	m.PendingSkill = selected[0]
	m.continuePendingWithSelectedSkills()
	return m, nil
}

func (m Model) updateBatchRootSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.BatchRootPicker.Handle(msg.String()) {
	case picker.Submit:
		selection := m.BatchRootPicker.Selection()
		if !selection.HasChoice {
			m.cancel("no duplicate root selected")
			return m, nil
		}
		choice := m.BatchRootChoices[selection.Cursor]
		m.PendingAction = ActionQuarantine
		m.PendingSkills = append([]inventory.Skill(nil), choice.Skills...)
		m.PendingSkill = choice.Skills[0]
		m.Message = fmt.Sprintf("Quarantine duplicate installs from %s", choice.Root)
		m.continuePendingWithSelectedSkills()
	case picker.Cancel:
		m.cancel("batch cleanup cancelled")
	}
	return m, nil
}

func (m Model) updateDraftSkillSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.DraftCursor < len(m.DraftChoices)-1 {
			m.DraftCursor++
		}
	case "k", "up":
		if m.DraftCursor > 0 {
			m.DraftCursor--
		}
	case " ":
		if len(m.DraftChoices) > 0 {
			if m.DraftSelections == nil {
				m.DraftSelections = map[int]bool{}
			}
			m.DraftSelections[m.DraftCursor] = !m.DraftSelections[m.DraftCursor]
		}
	case "enter":
		selected := m.selectedDraftSkills()
		if len(selected) < 2 {
			m.setStatus("select at least two skills for a merged draft")
			return m, nil
		}
		m.PendingSkills = selected
		m.State = StateGeneratingDraft
		m.Message = fmt.Sprintf("Generating read-only merged SKILL.md preview for %d selected skills", len(selected))
		return m, m.draftLifecycle.start(m.Actions, selected)
	case "esc", "q":
		m.cancel("merge draft cancelled")
	}
	return m, nil
}

func (m Model) updateDraftGeneration(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.draftLifecycle.cancel()
		m.cancel("merge draft cancelled")
	}
	return m, nil
}

func (m Model) handleDraftMergeResult(msg draftMergeResultMsg) Model {
	if !m.draftLifecycle.accept(msg) || m.State != StateGeneratingDraft {
		return m
	}
	if msg.Err != nil {
		m.fail(fmt.Errorf("merge draft unavailable: %w", msg.Err))
		return m
	}
	m.PendingSkills = append([]inventory.Skill(nil), msg.Skills...)
	m.DraftPreview = msg.Result.Markdown
	m.DraftProvider = msg.Result.Provider
	m.DraftModel = msg.Result.Model
	m.DraftScroll = 0
	m.State = StatePreviewDraft
	m.Message = fmt.Sprintf("Read-only merged SKILL.md preview for %d selected skills", len(msg.Skills))
	return m
}

func (m Model) updateDraftPreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.DraftScroll < max(0, len(strings.Split(m.DraftPreview, "\n"))-1) {
			m.DraftScroll++
		}
	case "k", "up":
		if m.DraftScroll > 0 {
			m.DraftScroll--
		}
	case "esc", "q", "enter":
		m.complete("closed merge draft preview without writing files")
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
		outcome := m.Actions.Mutate(workbench.Request{Kind: workbench.Rename, Authorized: true, Snapshot: m.snapshot(), Targets: []inventory.Skill{m.PendingSkill}, NewName: m.Input})
		m.applyMutationOutcome(outcome)
		if err := outcome.Err(); err != nil {
			m.fail(err)
		} else {
			m.complete(fmt.Sprintf("renamed %s -> %s", outcome.RenamePreview.OldPath, outcome.RenamePreview.NewPath))
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
			m.InstallPicker = m.newInstallPicker(m.clampedDetailCursor(finding))
			m.State = StateSelectInstall
			m.Message = fmt.Sprintf("Choose the exact %s install to %s", finding.Title, actionVerb(action))
			return
		}
		if group, ok := m.selectedSkillGroup(); ok && len(group.Skills) > 1 {
			m.PendingFinding = analysis.Finding{Title: group.Name, Skills: group.Skills}
			m.PendingSkill = inventory.Skill{}
			m.InstallPicker = m.newInstallPicker(0)
			m.State = StateSelectInstall
			m.Message = fmt.Sprintf("Choose the exact %s install to %s", group.Name, actionVerb(action))
			return
		}
	}
	skill, ok := m.selectedSkill()
	if !ok {
		m.setStatus("no skill selected")
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
			m.setStatus("rename requires a new name")
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
		m.setStatus("no duplicate roots to batch clean")
		return
	}
	m.PendingAction = ActionQuarantine
	m.BatchRootChoices = choices
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, fmt.Sprintf("%s · %d duplicate installs", choice.Root, len(choice.Skills)))
	}
	m.BatchRootPicker = picker.New(labels, picker.Config{})
	m.State = StateSelectBatchRoot
	m.Message = "Choose a root to quarantine duplicate installs from"
}

func (m *Model) beginDraftMergeAction() {
	choices, preselected := m.draftSkillChoices()
	if len(choices) == 0 {
		m.setStatus("no skills available for merge draft")
		return
	}
	m.DraftChoices = choices
	m.DraftSelections = preselected
	m.DraftCursor = firstSelectedIndex(preselected)
	m.DraftScroll = 0
	m.State = StateSelectDraftSkills
	m.Message = "Select any skills to combine into a preview-only SKILL.md draft"
}

func (m *Model) beginRestoreAction() {
	skill, ok := m.selectedSkill()
	if !ok {
		m.setStatus("select a destination skill/root before restore")
		return
	}
	choices, err := m.Actions.QuarantinedSkills()
	if err != nil {
		m.fail(err)
		return
	}
	m.PendingSkill = skill
	m.RestoreChoices = choices
	m.RestorePicker = picker.New(choices, picker.Config{EmptyLabel: "No quarantined skills found"})
	m.State = StateSelectRestore
	m.Message = fmt.Sprintf("Restore quarantined skill into %s", skill.Root)
}

func (m *Model) keepSelected() {
	skill, ok := m.selectedSkill()
	if !ok {
		m.setStatus("no skill selected")
		return
	}
	if err := m.Actions.KeepSkill(skill); err != nil {
		m.fail(err)
		return
	}
	m.setStatus("kept " + skill.Name)
}

func (m *Model) ignoreSelectedFinding() {
	if m.Mode != ViewFindings || len(m.Findings) == 0 {
		m.setStatus("ignore finding is only available in findings view")
		return
	}
	finding, ok := m.selectedFinding()
	if !ok {
		m.setStatus("no finding selected")
		return
	}
	if err := m.Actions.IgnoreFinding(finding); err != nil {
		m.fail(err)
		return
	}
	m.setStatus("ignored " + finding.Title)
}

func (m *Model) selectedSkill() (inventory.Skill, bool) {
	if m.Mode == ViewGuidedReview {
		item, ok := m.GuidedReview.Current()
		return item.Target, ok
	}
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

func actionResultStatus(action string, outcome workbench.Outcome) string {
	completed := len(outcome.Paths)
	stale := len(outcome.Missing)
	parts := make([]string, 0, 2)
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%s %d %s", action, completed, installWord(completed)))
	}
	if stale > 0 {
		parts = append(parts, fmt.Sprintf("removed %d stale %s from inventory", stale, installWord(stale)))
	}
	if len(parts) == 0 {
		return action + " 0 installs"
	}
	return strings.Join(parts, "; ")
}

func actionFailureStatus(completed string, err error) string {
	if completed == "" || strings.HasSuffix(completed, " 0 installs") {
		return "error: " + err.Error()
	}
	return completed + "; error: " + err.Error()
}

func installWord(count int) string {
	if count == 1 {
		return "install"
	}
	return "installs"
}

func (m Model) snapshot() inventorysnapshot.Snapshot {
	return inventorysnapshot.Snapshot{Skills: m.Skills, Findings: m.Findings}
}

func (m *Model) applyMutationOutcome(outcome workbench.Outcome) {
	m.Skills = outcome.Snapshot.Skills
	m.Findings = outcome.Snapshot.Findings
	m.SkillGroups = groupedSkills(m.Skills)
	if m.Cursor >= m.itemCount() {
		m.Cursor = max(0, m.itemCount()-1)
	}
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
	m.draftLifecycle.cancel()
	m.State = StateNormal
	m.PendingAction = ActionNone
	m.PendingSkill = inventory.Skill{}
	m.PendingSkills = nil
	m.PendingFinding = analysis.Finding{}
	m.InstallPicker = picker.Model{}
	m.RestorePicker = picker.Model{}
	m.BatchRootPicker = picker.Model{}
	m.BatchRootChoices = nil
	m.RestoreChoices = nil
	m.Input = ""
	m.Message = ""
	m.RenamePreview = fsactions.RenamePreview{}
	m.DraftCursor = 0
	m.DraftScroll = 0
	m.DraftChoices = nil
	m.DraftSelections = nil
	m.DraftPreview = ""
	m.DraftProvider = ""
	m.DraftModel = ""
	m.guidedReviewDecisionPending = false
}

func (m *Model) complete(status string) {
	m.resetInteraction()
	m.setStatus(status)
}

func (m *Model) cancel(status string) {
	m.resetInteraction()
	m.setStatus(status)
}

func (m *Model) fail(err error) {
	m.resetInteraction()
	m.setStatus("error: " + err.Error())
}

const (
	minimumWidth  = 80
	minimumHeight = 18
)

func (m Model) View() string {
	width, height := m.dimensions()
	theme := ui.DefaultTheme()
	if width < minimumWidth || height < minimumHeight {
		return renderMinimumSizeGate(theme, width, height)
	}
	headerHeight := 2
	keybarHeight := 1
	feedbackLines := []string(nil)
	maxFeedbackHeight := min(5, max(0, height-headerHeight-keybarHeight-8))
	showFeedback := m.State != StateHelp && m.State != StateFeedback && (m.State == StateNormal || m.StatusContext == m.State)
	if showFeedback && maxFeedbackHeight > 0 {
		feedbackLines = m.renderFeedback(theme, width, maxFeedbackHeight)
	}
	feedbackHeight := len(feedbackLines)
	bodyHeight := height - headerHeight - feedbackHeight - keybarHeight
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
	} else if m.Mode == ViewGuidedReview {
		body = m.renderGuidedReview(theme, width, bodyHeight)
	} else {
		left := theme.Panel.Width(leftWidth - 2).Height(bodyHeight - 2).Render(m.renderList(theme, leftWidth-4, bodyHeight-2))
		right := theme.Panel.Width(rightWidth - 2).Height(bodyHeight - 2).Render(m.renderDetails(theme, rightWidth-4, bodyHeight-2))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
	keybar := m.renderKeybar(theme, width)
	parts := []string{header}
	if feedbackHeight > 0 {
		parts = append(parts, strings.Join(feedbackLines, "\n"))
	}
	parts = append(parts, body, keybar)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
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
	return width, height
}

func renderMinimumSizeGate(theme ui.Theme, width, height int) string {
	message := strings.Join([]string{
		theme.AppTitle.Render("unlearn"),
		"",
		theme.Warning.Render("Terminal too small"),
		fmt.Sprintf("Resize to at least %d×%d.", minimumWidth, minimumHeight),
		fmt.Sprintf("Current size: %d×%d", width, height),
	}, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, message)
}

func (m Model) renderHeader(theme ui.Theme, width, height int) string {
	mode := "findings"
	switch m.Mode {
	case ViewSkills:
		mode = "skills"
	case ViewGuidedReview:
		mode = "guided review"
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
	line := padBetween(title, lipgloss.JoinHorizontal(lipgloss.Center, stats...), width)
	sep := theme.Muted.Render(strings.Repeat("─", max(0, width)))
	return strings.Join(ui.PadLines([]string{ui.Truncate(line, width), sep}, height), "\n")
}

func (m Model) renderList(theme ui.Theme, width, height int) string {
	lines := []string{theme.PanelTitle.Render(m.listTitle())}
	if m.itemCount() == 0 {
		if m.Mode == ViewFindings {
			lines = append(lines, "", theme.Section.Render("No cleanup findings"))
			if len(m.Skills) > 0 {
				lines = append(lines, theme.Muted.Render("s skills · browse the inventory"))
			} else {
				lines = append(lines, theme.Muted.Render("Run unlearn scan to build the inventory."))
			}
		} else {
			lines = append(lines, "", theme.Section.Render("No skills found"), theme.Muted.Render("Run unlearn scan to discover installed skills."))
		}
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
	if finding.Type == analysis.FindingSkillQuality {
		meta += " · advisory"
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
	modalWidth, contentWidth, contentHeight := modalContentDimensions(width, height, m.State)
	lines := m.renderInteraction(theme, contentWidth, contentHeight)
	modal := theme.Modal.Width(modalWidth - 2).Render(strings.Join(fitLinesPreservingTail(lines, contentHeight, 3), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func modalContentDimensions(width, height int, state InteractionState) (int, int, int) {
	modalWidth := width - 16
	if state == StatePreviewDraft && width > 100 {
		modalWidth = width - 10
	}
	if modalWidth > 110 {
		modalWidth = 110
	}
	if modalWidth < 52 {
		modalWidth = width - 4
	}
	return modalWidth, max(1, modalWidth-6), max(1, height-4)
}

func (m Model) renderDetails(theme ui.Theme, width, height int) string {
	lines := []string{theme.PanelTitle.Render("Details")}
	if m.itemCount() == 0 {
		lines = append(lines, "", theme.Muted.Render("Nothing selected"))
		return strings.Join(ui.PadLines(lines, height), "\n")
	}
	if m.State != StateNormal {
		lines = append(lines, m.renderInteraction(theme, width, height-1)...)
		return strings.Join(ui.PadLines(ui.FitLines(lines, height), height), "\n")
	}
	if m.Mode == ViewFindings {
		lines = append(lines, m.renderFindingDetails(theme, width, height-1)...)
	} else {
		lines = append(lines, m.renderSkillGroupDetails(theme, width, height-1, m.SkillGroups[m.Cursor])...)
	}
	return strings.Join(ui.PadLines(ui.FitLines(lines, height), height), "\n")
}

func (m Model) renderInteraction(theme ui.Theme, width, height int) []string {
	if m.State == StateSkillStory {
		return m.renderSkillStory(theme, width, height)
	}
	if m.State == StateDiscoveryQuery || m.State == StateDiscoveryResults || m.State == StateDiscoveryInspect {
		return m.renderDiscovery(theme, width, height)
	}
	if m.State == StateHelp {
		return m.renderHelp(theme, width)
	}
	if m.State == StateFeedback {
		return m.renderFeedbackDetail(theme, width, height)
	}
	label := interactionTitle(m.State, len(m.selectedPendingSkills()))
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
		lines = appendPickerWindow(lines, theme.Muted.Render("↑/↓ choose · PgUp/PgDn scroll · enter select"), m.RestorePicker, theme, width, height)
	}
	if m.State == StateSelectBatchRoot {
		lines = appendPickerWindow(lines, theme.Muted.Render("↑/↓ choose · PgUp/PgDn scroll · enter select"), m.BatchRootPicker, theme, width, height)
	}
	if m.State == StateSelectInstall {
		lines = appendPickerWindow(lines, theme.Muted.Render("↑/↓ choose · space mark · PgUp/PgDn scroll · enter"), m.InstallPicker, theme, width, height)
	}
	if m.State == StateSelectDraftSkills {
		lines = append(lines, m.renderDraftSkillPicker(theme, width, height-len(lines)-3)...)
	}
	if m.State == StateGeneratingDraft {
		lines = append(lines, "", theme.Muted.Render("Generating preview… the dashboard will update when the LLM response is ready."))
	}
	if m.State == StatePreviewDraft {
		lines = append(lines, m.renderDraftPreview(theme, width, height-len(lines)-3)...)
	}
	if m.Input != "" || m.State == StateInputRename {
		lines = append(lines, "", theme.Muted.Render("Input"), theme.Accent.Render("› ")+ui.Truncate(m.Input, width-2))
	}
	lines = append(lines, "", theme.Muted.Render("Options"), optionLineForState(theme, m.State))
	return lines
}

func (m Model) renderDraftSkillPicker(theme ui.Theme, width, height int) []string {
	selectedCount := len(m.selectedDraftSkills())
	lines := []string{"", theme.Muted.Render(fmt.Sprintf("Use ↑/↓, space to select any skills, enter to generate (%d selected):", selectedCount))}
	rows := m.renderDraftChoiceRows(theme, width, max(1, height-1))
	lines = append(lines, rows...)
	return lines
}

func (m Model) renderDraftChoiceRows(theme ui.Theme, width, height int) []string {
	if len(m.DraftChoices) == 0 {
		return []string{theme.Muted.Render("  No skills available")}
	}
	var rows []string
	selectedLine := 0
	lastGrouped := false
	for i, choice := range m.DraftChoices {
		if choice.OverlapGrouped && !lastGrouped {
			rows = append(rows, theme.Section.Render(ui.Truncate("Overlap group from current finding", width)))
		}
		if !choice.OverlapGrouped && (lastGrouped || i == 0) {
			label := "All skills"
			if i > 0 {
				label = "Other skills"
			}
			rows = append(rows, theme.Section.Render(ui.Truncate(label, width)))
		}
		lastGrouped = choice.OverlapGrouped
		mark := "[ ]"
		if m.DraftSelections[i] {
			mark = "[x]"
		}
		prefix := "  "
		style := theme.Row
		if i == m.DraftCursor {
			prefix = "▸ "
			style = theme.SelectedRow.Width(width)
			selectedLine = len(rows)
		}
		label := fmt.Sprintf("%s %s", mark, draftChoiceLabel(choice.Skill))
		rows = append(rows, style.Render(ui.Truncate(prefix+label, width)))
	}
	return windowLines(rows, height, selectedLine)
}

func (m Model) renderDraftPreview(theme ui.Theme, width, height int) []string {
	provider := emptyDetailLabel(m.DraftProvider)
	model := emptyDetailLabel(m.DraftModel)
	providerText := fmt.Sprintf("Generated by %s/%s. Preview only; no files were written.", provider, model)
	lines := []string{""}
	for _, line := range ui.Wrap(providerText, width) {
		lines = append(lines, theme.Muted.Render(line))
	}
	lines = append(lines, "")
	if len(lines) >= height {
		return ui.FitLines(lines, height)
	}

	previewLines := strings.Split(strings.TrimSpace(m.DraftPreview), "\n")
	start := min(m.DraftScroll, max(0, len(previewLines)-1))
	remaining := height - len(lines)
	if start > 0 && remaining > 0 {
		lines = append(lines, theme.Muted.Render("… above"))
		remaining--
	}
	visible := remaining
	if start+visible < len(previewLines) && visible > 0 {
		visible--
	}
	end := min(len(previewLines), start+visible)
	for _, line := range previewLines[start:end] {
		lines = append(lines, theme.Row.Render(ui.Truncate(line, width)))
	}
	if end < len(previewLines) && len(lines) < height {
		lines = append(lines, theme.Muted.Render("… more"))
	}
	return ui.FitLines(lines, height)
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
	if finding.Type == analysis.FindingSkillQuality {
		lines = append(lines, theme.Muted.Render(ui.Truncate("• Advisory only; safe fixes ignore this finding", width)))
	}
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
	case analysis.FindingSkillQuality:
		return theme.Badge.Render(label)
	case analysis.FindingDuplicate:
		return theme.BadgeSuccess.Render(label)
	default:
		return theme.Badge.Render(label)
	}
}

func (m Model) renderKeybar(theme ui.Theme, width int) string {
	limit := max(1, width-2)
	contentLimit := max(1, limit-2)
	parts := m.keyParts()
	if m.State != StateNormal {
		return theme.Keybar.Width(limit).Render(ui.Truncate(renderKeyParts(theme, parts, contentLimit), contentLimit))
	}

	reservedParts := []keyPart{{"?", "help"}, {"q", "quit"}}
	if m.Mode == ViewGuidedReview {
		reservedParts = []keyPart{{"?", "help"}}
	}
	reserved := renderKeyParts(theme, reservedParts, contentLimit)
	leftLimit := max(0, contentLimit-lipgloss.Width(reserved)-2)
	left := renderKeyParts(theme, parts, leftLimit)
	line := reserved
	if left != "" {
		line = padBetween(left, reserved, contentLimit)
	}
	return theme.Keybar.Width(limit).Render(ui.Truncate(line, contentLimit))
}

func renderKeyParts(theme ui.Theme, parts []keyPart, limit int) string {
	var out []string
	used := 0
	for i, part := range parts {
		rendered := theme.Key.Render(part.Key) + " " + part.Label
		separator := ""
		if len(out) > 0 {
			separator = "  "
		}
		if used+lipgloss.Width(separator+rendered) > limit {
			ellipsis := theme.Muted.Render("  …")
			for lipgloss.Width(strings.Join(out, "")+ellipsis) > limit && len(out) > 1 {
				out = out[:len(out)-1]
			}
			if i < len(parts) && lipgloss.Width(strings.Join(out, "")+ellipsis) <= limit {
				out = append(out, ellipsis)
			}
			break
		}
		out = append(out, separator+rendered)
		used += lipgloss.Width(separator + rendered)
	}
	return strings.Join(out, "")
}

type keyPart struct{ Key, Label string }

func (m Model) keyParts() []keyPart {
	if m.State != StateNormal {
		switch m.State {
		case StateWriteGate, StateConfirmQuarantine, StateConfirmDelete, StatePreviewRename:
			return []keyPart{{"y", confirmationLabel(m.State)}, {"n", "cancel"}, {"esc", "back"}}
		case StateSelectInstall:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "select"}, {"esc", "cancel"}}
		case StateSelectRestore:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "restore"}, {"esc", "cancel"}}
		case StateSelectBatchRoot:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "preview"}, {"esc", "cancel"}}
		case StateSelectDraftSkills:
			return []keyPart{{"↑↓/jk", "choose"}, {"space", "toggle"}, {"enter", "generate"}, {"esc", "cancel"}}
		case StateGeneratingDraft:
			return []keyPart{{"esc", "cancel"}}
		case StatePreviewDraft:
			return []keyPart{{"↑↓/jk", "scroll"}, {"esc", "close"}}
		case StateSkillStory:
			return []keyPart{{"↑↓/jk", "scroll"}, {"pgup/pgdown", "page"}, {"esc", "back"}}
		case StateDiscoveryQuery:
			return []keyPart{{"type", "task"}, {"enter", "search"}, {"esc", "back"}}
		case StateDiscoveryResults:
			return []keyPart{{"↑↓/jk", "choose"}, {"enter", "inspect"}, {"e", "edit"}, {"esc", "back"}}
		case StateDiscoveryInspect:
			return []keyPart{{"↑↓/jk", "scroll"}, {"e", "edit"}, {"esc", "results"}}
		case StateInputRename:
			return []keyPart{{"type", "input"}, {"enter", "submit"}, {"esc", "cancel"}}
		case StateHelp:
			return []keyPart{{"esc", "close"}, {"?", "close"}}
		case StateFeedback:
			return []keyPart{{"↑↓/jk", "scroll"}, {"pgup/pgdown", "page"}, {"esc", "close"}, {"x", "dismiss"}}
		}
	}
	if m.Mode == ViewGuidedReview {
		return []keyPart{{"k", "keep"}, {"q", "quarantine"}, {"l", "revisit later"}, {"esc", "back"}}
	}
	parts := []keyPart{{"↑↓/jk", "move"}}
	if m.Mode == ViewFindings {
		parts = append(parts, keyPart{"s", "skills"})
	} else {
		parts = append(parts, keyPart{"f", "findings"})
	}
	parts = append(parts, keyPart{"ctrl+q", "quarantine"}, keyPart{"ctrl+d", "delete"}, keyPart{"ctrl+k", "keep"}, keyPart{"d", "discover"})
	if m.Mode == ViewFindings {
		parts = append(parts, keyPart{"ctrl+g", "ignore"})
	} else {
		parts = append(parts, keyPart{"enter", "story"})
	}
	parts = append(parts, keyPart{"ctrl+r", "rename"}, keyPart{"ctrl+u", "restore"}, keyPart{"ctrl+b", "batch"}, keyPart{"m", "draft merge"}, keyPart{"v", "guided review"})
	return parts
}

func (m Model) itemCount() int {
	switch m.Mode {
	case ViewFindings:
		return len(m.Findings)
	case ViewSkills:
		return len(m.SkillGroups)
	case ViewGuidedReview:
		if _, ok := m.GuidedReview.Current(); ok {
			return 1
		}
	}
	return 0
}

func fitLinesPreservingTail(lines []string, height, tailHeight int) []string {
	if height <= 0 || len(lines) <= height {
		return ui.FitLines(lines, height)
	}
	tailHeight = min(max(0, tailHeight), min(height, len(lines)))
	tailStart := len(lines) - tailHeight
	head := ui.FitLines(lines[:tailStart], height-tailHeight)
	return append(head, lines[tailStart:]...)
}

func appendPickerWindow(lines []string, instruction string, model picker.Model, theme ui.Theme, width, height int) []string {
	lines = append(lines, "", instruction)
	model.Resize(width, max(1, height-len(lines)-3))
	return append(lines, model.View(theme)...)
}

func wrapPreservingText(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	remaining := []rune(strings.ReplaceAll(value, "\n", " "))
	for len(remaining) > 0 {
		end := 0
		for end < len(remaining) && lipgloss.Width(string(remaining[:end+1])) <= width {
			end++
		}
		if end == 0 {
			end = 1
		}
		breakAt := end
		if end < len(remaining) {
			for i := end - 1; i > 0; i-- {
				if remaining[i] == '/' || remaining[i] == ' ' {
					breakAt = i + 1
					break
				}
			}
		}
		lines = append(lines, strings.TrimRight(string(remaining[:breakAt]), " "))
		remaining = remaining[breakAt:]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
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

func (m Model) newInstallPicker(cursor int) picker.Model {
	choices := m.pendingInstallChoices()
	labels := make([]string, 0, len(choices))
	for _, skill := range choices {
		labels = append(labels, installChoiceLabel(skill))
	}
	config := picker.Config{Cursor: cursor, MultiSelect: true}
	if m.canActOnAllInstalls() {
		config.ExtraChoice = fmt.Sprintf("All %d installs", len(choices))
	}
	return picker.New(labels, config)
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
		return theme.Key.Render("y") + " " + confirmationLabel(state) + "  " + theme.Key.Render("n") + " cancel  " + theme.Key.Render("esc") + " back"
	case StateSelectInstall:
		return theme.Key.Render("enter") + " select highlighted install  " + theme.Key.Render("esc") + " cancel"
	case StateSelectRestore:
		return theme.Key.Render("enter") + " restore highlighted skill  " + theme.Key.Render("esc") + " cancel"
	case StateSelectBatchRoot:
		return theme.Key.Render("enter") + " preview root cleanup  " + theme.Key.Render("esc") + " cancel"
	case StateSelectDraftSkills:
		return theme.Key.Render("space") + " toggle  " + theme.Key.Render("enter") + " generate preview  " + theme.Key.Render("esc") + " cancel"
	case StateGeneratingDraft:
		return theme.Key.Render("esc") + " cancel"
	case StatePreviewDraft:
		return theme.Key.Render("↑↓/jk") + " scroll  " + theme.Key.Render("esc") + " close"
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
		for _, line := range wrapPreservingText("    path "+path, width) {
			lines = append(lines, theme.Muted.Render(line))
		}
	}
	if skill.Provenance != "" {
		for _, line := range wrapPreservingText("    provenance "+skill.Provenance, width) {
			lines = append(lines, theme.Muted.Render(line))
		}
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

func (m Model) draftSkillChoices() ([]draftSkillChoice, map[int]bool) {
	preselectedNames := map[string]bool{}
	if finding, ok := m.selectedFinding(); ok && finding.Type == analysis.FindingOverlap {
		for _, skill := range finding.Skills {
			preselectedNames[strings.ToLower(strings.TrimSpace(skill.Name))] = true
		}
	}
	choices := make([]draftSkillChoice, 0, len(m.SkillGroups))
	add := func(grouped bool) {
		for _, group := range m.SkillGroups {
			name := strings.ToLower(strings.TrimSpace(group.Name))
			if preselectedNames[name] != grouped {
				continue
			}
			choices = append(choices, draftSkillChoice{Skill: group.Representative, OverlapGrouped: grouped})
		}
	}
	add(true)
	add(false)
	selections := map[int]bool{}
	for i, choice := range choices {
		if choice.OverlapGrouped {
			selections[i] = true
		}
	}
	return choices, selections
}

func (m Model) selectedDraftSkills() []inventory.Skill {
	var selected []inventory.Skill
	for i, choice := range m.DraftChoices {
		if m.DraftSelections[i] {
			selected = append(selected, choice.Skill)
		}
	}
	return selected
}

func firstSelectedIndex(selections map[int]bool) int {
	for i := 0; ; i++ {
		selected, ok := selections[i]
		if !ok {
			if i > len(selections) {
				return 0
			}
			continue
		}
		if selected {
			return i
		}
	}
}

func draftChoiceLabel(skill inventory.Skill) string {
	label := skill.Name
	if skill.Description != "" && !broadGenericDescription(skill.Description) {
		label += " · " + ui.Truncate(skill.Description, 48)
	}
	if skill.Root != "" {
		label += " · " + skill.Root
	}
	return label
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
