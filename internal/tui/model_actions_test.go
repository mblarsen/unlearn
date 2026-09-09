package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/workbench"
)

type fakeActionService struct {
	writeRoots       map[string]bool
	kept             []string
	ignored          []string
	quarantined      []string
	quarantinedRoot  []string
	deleted          []string
	deletedRoot      []string
	renamed          []string
	restored         []string
	quarantinedList  []string
	deleteTypedName  string
	deleteBatchToken string
	deleteResult     fsactions.Result
	deleteErr        error
	draftMarkdown    string
	draftErr         error
	draftSelected    []string
}

func (f *fakeActionService) KeepSkill(skill inventory.Skill) error {
	f.kept = append(f.kept, skill.Name)
	return nil
}
func (f *fakeActionService) IgnoreFinding(finding analysis.Finding) error {
	f.ignored = append(f.ignored, finding.ID)
	return nil
}
func (f *fakeActionService) FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool) {
	for _, skill := range skills {
		if !f.writeRoots[skill.Root] {
			return skill, true
		}
	}
	return inventory.Skill{}, false
}
func (f *fakeActionService) AllowWrite(root string) error {
	if f.writeRoots == nil {
		f.writeRoots = map[string]bool{}
	}
	f.writeRoots[root] = true
	return nil
}
func (f *fakeActionService) Mutate(request workbench.Request) workbench.Outcome {
	outcome := workbench.Outcome{Snapshot: request.Snapshot.Clone()}
	switch request.Kind {
	case workbench.Quarantine:
		outcome.Removed = append([]inventory.Skill(nil), request.Targets...)
		for _, skill := range request.Targets {
			f.quarantined = append(f.quarantined, skill.Name)
			f.quarantinedRoot = append(f.quarantinedRoot, skill.Root)
			outcome.Paths = append(outcome.Paths, "/quarantine/"+skill.Name)
		}
		outcome.Snapshot = inventorysnapshot.Remove(outcome.Snapshot, outcome.Removed)
	case workbench.Delete:
		f.deleteTypedName = request.Confirmation.TypedName
		f.deleteBatchToken = request.Confirmation.BatchToken
		result := fsactions.Result{Skills: append([]inventory.Skill(nil), request.Targets...)}
		for _, skill := range request.Targets {
			f.deleted = append(f.deleted, skill.Name)
			f.deletedRoot = append(f.deletedRoot, skill.Root)
			result.Paths = append(result.Paths, skill.EncounteredPath)
		}
		if f.deleteResult.Skills != nil || f.deleteErr != nil {
			result = f.deleteResult
		}
		outcome.Removed = result.Skills
		outcome.Missing = result.Missing
		outcome.Paths = result.Paths
		outcome.Snapshot = inventorysnapshot.Remove(outcome.Snapshot, outcome.Removed)
		if f.deleteErr != nil {
			outcome.Failures = append(outcome.Failures, workbench.Failure{Phase: workbench.FilesystemPhase, Err: f.deleteErr})
		}
	case workbench.Rename:
		skill := request.Targets[0]
		f.renamed = append(f.renamed, skill.Name+":"+request.NewName)
		preview := fsactions.PreviewRename(skill, request.NewName)
		outcome.RenamePreview = preview
		outcome.Snapshot, _ = inventorysnapshot.Rename(outcome.Snapshot, skill, request.NewName, preview.NewPath)
	case workbench.Restore:
		f.restored = append(f.restored, request.RestoreName+":"+request.DestinationRoot)
		path := request.DestinationRoot + "/" + request.RestoreName
		outcome.Paths = []string{path}
		restored := inventory.Skill{Name: request.RestoreName, Root: request.DestinationRoot, EncounteredPath: path}
		outcome.Restored = &restored
		outcome.Snapshot = inventorysnapshot.Add(outcome.Snapshot, restored)
	}
	return outcome
}
func (f *fakeActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (f *fakeActionService) QuarantinedSkills() ([]string, error) {
	return append([]string(nil), f.quarantinedList...), nil
}
func (f *fakeActionService) DraftMerge(_ context.Context, skills []inventory.Skill) (llm.DraftResult, error) {
	f.draftSelected = nil
	for _, skill := range skills {
		f.draftSelected = append(f.draftSelected, skill.Name)
	}
	if f.draftErr != nil {
		return llm.DraftResult{}, f.draftErr
	}
	markdown := f.draftMarkdown
	if markdown == "" {
		markdown = "---\nname: merged-skill\ndescription: Combined skill\n---\n\n# Combined"
	}
	return llm.DraftResult{Markdown: markdown, Provider: "fake", Model: "test"}, nil
}

func TestDashboardKeepAndIgnoreFindingActions(t *testing.T) {
	service := &fakeActionService{}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+k"))
	m = updated.(Model)
	if len(service.kept) != 1 || service.kept[0] != "alpha" {
		t.Fatalf("kept=%v", service.kept)
	}
	updated, _ = m.Update(key("ctrl+g"))
	m = updated.(Model)
	if len(service.ignored) != 1 || service.ignored[0] != "duplicate:alpha" {
		t.Fatalf("ignored=%v", service.ignored)
	}
}

func TestDashboardQuarantineRequiresWriteGateAndConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+q"))
	m = updated.(Model)
	if m.State != StateWriteGate || !strings.Contains(m.View(), "this install") || !strings.Contains(m.View(), "/root/alpha") {
		t.Fatalf("expected exact write gate, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if m.State != StateConfirmQuarantine || !strings.Contains(m.View(), "/root/alpha") {
		t.Fatalf("expected quarantine confirmation with target, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if m.State != StateNormal || len(service.quarantined) != 1 || service.quarantined[0] != "alpha" {
		t.Fatalf("state=%v quarantined=%v", m.State, service.quarantined)
	}
}

func TestDashboardCanMarkMultipleDuplicateInstalls(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/two": true, "/three": true}}
	skills := []inventory.Skill{
		{Name: "simplify", Root: "/one", EncounteredPath: "/one/simplify", PrimaryPath: "/one/simplify/SKILL.md"},
		{Name: "simplify", Root: "/two", EncounteredPath: "/two/simplify", PrimaryPath: "/two/simplify/SKILL.md"},
		{Name: "simplify", Root: "/three", EncounteredPath: "/three/simplify", PrimaryPath: "/three/simplify/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:simplify", Title: "simplify", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateConfirmDelete || len(m.PendingSkills) != 2 || !strings.Contains(m.View(), "all 2 installs") {
		t.Fatalf("expected two selected installs for delete, state=%v pending=%v view=%s", m.State, len(m.PendingSkills), m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.deletedRoot) != 2 || service.deletedRoot[0] != "/two" || service.deletedRoot[1] != "/three" {
		t.Fatalf("deleted roots=%v", service.deletedRoot)
	}
	if service.deleteBatchToken != fsactions.BatchDeleteConfirmation(servicePendingDeletedSkills(service)) {
		t.Fatalf("batch token=%q", service.deleteBatchToken)
	}
}

func TestDashboardBatchDuplicatesByRoot(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/drop": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/keep", EncounteredPath: "/keep/alpha"},
		{Name: "alpha", Root: "/drop", EncounteredPath: "/drop/alpha"},
		{Name: "beta", Root: "/keep", EncounteredPath: "/keep/beta"},
		{Name: "beta", Root: "/drop", EncounteredPath: "/drop/beta"},
	}
	findings := []analysis.Finding{
		{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills[:2]},
		{ID: "duplicate:beta", Title: "beta", Type: analysis.FindingDuplicate, Skills: skills[2:]},
	}
	m := NewWithActions(skills, findings, service)
	updated, _ := m.Update(key("ctrl+b"))
	m = updated.(Model)
	if m.State != StateSelectBatchRoot || !strings.Contains(m.View(), "/drop") {
		t.Fatalf("expected batch root picker:\n%s", m.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateConfirmQuarantine || len(m.PendingSkills) != 2 {
		t.Fatalf("expected batch quarantine preview, state=%v pending=%d view=%s", m.State, len(m.PendingSkills), m.View())
	}
}

func TestDashboardCanQuarantineAllDuplicateInstalls(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/one": true, "/two": true}}
	skills := []inventory.Skill{
		{Name: "cloudflare", Root: "/one", EncounteredPath: "/one/cloudflare", PrimaryPath: "/one/cloudflare/SKILL.md"},
		{Name: "cloudflare", Root: "/two", EncounteredPath: "/two/cloudflare", PrimaryPath: "/two/cloudflare/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:cloudflare", Title: "cloudflare", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(key("ctrl+q"))
	m = updated.(Model)
	if !strings.Contains(m.View(), "All 2 installs") {
		t.Fatalf("expected all option:\n%s", m.View())
	}
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateConfirmQuarantine || !strings.Contains(m.View(), "all 2 installs") {
		t.Fatalf("expected all quarantine confirmation, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.quarantinedRoot) != 2 || m.itemCount() != 0 {
		t.Fatalf("quarantined roots=%v itemCount=%d", service.quarantinedRoot, m.itemCount())
	}
}

func TestDashboardTabFocusesDuplicateInstallDefaultAction(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Description: "Alpha in one", Root: "/one", EncounteredPath: "/one/alpha", PrimaryPath: "/one/alpha/SKILL.md"},
		{Name: "alpha", Description: "Alpha in two", Root: "/two", EncounteredPath: "/two/alpha", PrimaryPath: "/two/alpha/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "Alpha in two") {
		t.Fatalf("expected second install description after tab:\n%s", view)
	}
	updated, _ = m.Update(key("ctrl+d"))
	m = updated.(Model)
	selection := m.InstallPicker.Selection()
	if m.State != StateSelectInstall || selection.Cursor != 1 {
		t.Fatalf("expected action chooser to focus detail-selected install, state=%v cursor=%d", m.State, selection.Cursor)
	}
}

func TestDashboardRequiresInstallChoiceForDuplicateFindingActions(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", EncounteredPath: "/one/alpha", PrimaryPath: "/one/alpha/SKILL.md"},
		{Name: "alpha", Root: "/two", EncounteredPath: "/two/alpha", PrimaryPath: "/two/alpha/SKILL.md"},
		{Name: "alpha", Root: "/three", EncounteredPath: "/three/alpha", PrimaryPath: "/three/alpha/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(key("ctrl+q"))
	m = updated.(Model)
	if m.State != StateSelectInstall || !strings.Contains(m.View(), "CHOOSE EXACT INSTALL") || !strings.Contains(m.View(), "/one/alpha") || !strings.Contains(m.View(), "/three/alpha") {
		t.Fatalf("expected install chooser before quarantine:\n%s", m.View())
	}
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateConfirmQuarantine || m.PendingSkill.Root != "/two" {
		t.Fatalf("expected selected /two confirm, state=%v skill=%#v view=%s", m.State, m.PendingSkill, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.quarantinedRoot) != 1 || service.quarantinedRoot[0] != "/two" {
		t.Fatalf("quarantined roots=%v", service.quarantinedRoot)
	}
	if strings.Contains(m.View(), "3 installs") || !strings.Contains(m.View(), "2 installs") {
		t.Fatalf("quarantine should update duplicate count in current view:\n%s", m.View())
	}
}

func TestDashboardDeleteUpdatesDuplicateFindingCount(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", EncounteredPath: "/one/alpha", PrimaryPath: "/one/alpha/SKILL.md"},
		{Name: "alpha", Root: "/two", EncounteredPath: "/two/alpha", PrimaryPath: "/two/alpha/SKILL.md"},
		{Name: "alpha", Root: "/three", EncounteredPath: "/three/alpha", PrimaryPath: "/three/alpha/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.deletedRoot) != 1 || service.deletedRoot[0] != "/two" {
		t.Fatalf("deleted roots=%v", service.deletedRoot)
	}
	view := m.View()
	if strings.Contains(view, "3 installs") || !strings.Contains(view, "2 installs") {
		t.Fatalf("delete should update duplicate count in current view:\n%s", view)
	}
}

func TestDashboardDeleteClearsAllMissingInstallsWithStaleFeedback(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/one": true, "/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", EncounteredPath: "/one/alpha"},
		{Name: "alpha", Root: "/two", EncounteredPath: "/two/alpha"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	service.deleteResult = fsactions.Result{Skills: skills, Missing: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)

	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)

	if len(m.Skills) != 0 || len(m.Findings) != 0 {
		t.Fatalf("stale installs remain: skills=%#v findings=%#v", m.Skills, m.Findings)
	}
	if m.Status != "removed 2 stale installs from inventory" {
		t.Fatalf("status=%q", m.Status)
	}
}

func TestDashboardDeleteReconcilesCompletedSkillsAfterPartialFailure(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/one": true, "/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", EncounteredPath: "/one/alpha"},
		{Name: "alpha", Root: "/two", EncounteredPath: "/two/alpha"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	service.deleteResult = fsactions.Result{Skills: []inventory.Skill{skills[0]}, Paths: []string{skills[0].EncounteredPath}}
	service.deleteErr = fmt.Errorf("delete %s: permission denied", skills[1].EncounteredPath)
	m := NewWithActions(skills, []analysis.Finding{finding}, service)

	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)

	if len(m.Skills) != 1 || m.Skills[0].Root != "/two" {
		t.Fatalf("skills after partial failure=%#v", m.Skills)
	}
	if !strings.Contains(m.Status, "permission denied") || !strings.Contains(m.Status, "deleted 1") {
		t.Fatalf("partial failure status=%q", m.Status)
	}
}

func TestDashboardDeleteUsesModalConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	if m.State != StateConfirmDelete || !strings.Contains(m.View(), "y delete") || strings.Contains(m.View(), "type") {
		t.Fatalf("expected delete confirmation modal, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if service.deleteTypedName != "alpha" || len(service.deleted) != 1 {
		t.Fatalf("typed=%q deleted=%v", service.deleteTypedName, service.deleted)
	}
}

func TestDashboardRenameDryRunAndConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+r"))
	m = updated.(Model)
	for _, r := range "beta" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StatePreviewRename || !strings.Contains(m.View(), "Rename dry run") {
		t.Fatalf("expected rename preview, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.renamed) != 1 || service.renamed[0] != "alpha:beta" {
		t.Fatalf("renamed=%v", service.renamed)
	}
}

func TestDashboardRenameWarnsForSymlinkedSkill(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	skill := inventory.Skill{Name: "alpha", Root: "/root", EncounteredPath: "/root/alpha", PrimaryPath: "/root/alpha/SKILL.md", IsSymlink: true}
	m := NewWithActions([]inventory.Skill{skill}, nil, service)
	m.Mode = ViewSkills
	updated, _ := m.Update(key("ctrl+r"))
	m = updated.(Model)
	for _, r := range "beta" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StatePreviewRename || !strings.Contains(m.View(), "suggested action: quarantine") {
		t.Fatalf("expected warning preview, state=%v view=%s", m.State, m.View())
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.renamed) != 0 || !strings.Contains(m.Status, "suggested action: quarantine") {
		t.Fatalf("renamed=%v status=%q", service.renamed, m.Status)
	}
}

func TestDashboardRestoreUsesPopupSelection(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}, quarantinedList: []string{"old", "older"}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+u"))
	m = updated.(Model)
	if m.State != StateSelectRestore || !strings.Contains(m.View(), "RESTORE SKILL") || !strings.Contains(m.View(), "old") {
		t.Fatalf("expected restore chooser:\n%s", m.View())
	}
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if len(service.restored) != 1 || service.restored[0] != "older:/root" {
		t.Fatalf("restored=%v", service.restored)
	}
}

func TestDashboardDraftMergePickerAllowsArbitrarySkills(t *testing.T) {
	service := &fakeActionService{}
	skills := []inventory.Skill{
		{Name: "alpha", Description: "Alpha workflow", Root: "/one", Body: "alpha body"},
		{Name: "beta", Description: "Beta workflow", Root: "/two", Body: "beta body"},
		{Name: "gamma", Description: "Gamma workflow", Root: "/three", Body: "gamma body"},
	}
	m := NewWithActions(skills, nil, service)
	m.Mode = ViewSkills
	updated, _ := m.Update(key("m"))
	m = updated.(Model)
	if m.State != StateSelectDraftSkills || !strings.Contains(m.View(), "All skills") || !strings.Contains(m.View(), "alpha") || !strings.Contains(m.View(), "gamma") {
		t.Fatalf("expected arbitrary skill picker:\n%s", m.View())
	}
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateGeneratingDraft || cmd == nil {
		t.Fatalf("expected async draft generation, state=%v cmd nil=%v", m.State, cmd == nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.State != StatePreviewDraft || len(service.draftSelected) != 2 || service.draftSelected[0] != "alpha" || service.draftSelected[1] != "beta" {
		t.Fatalf("state=%v selected=%v view=%s", m.State, service.draftSelected, m.View())
	}
	if !strings.Contains(m.View(), "READ-ONLY MERGE DRAFT") || !strings.Contains(m.View(), "Preview only") {
		t.Fatalf("expected read-only preview:\n%s", m.View())
	}
}

func TestDashboardDraftMergeGroupsAndPreselectsCurrentOverlap(t *testing.T) {
	service := &fakeActionService{}
	skills := []inventory.Skill{
		{Name: "alpha", Description: "Alpha workflow", Root: "/one"},
		{Name: "beta", Description: "Beta workflow", Root: "/two"},
		{Name: "gamma", Description: "Gamma workflow", Root: "/three"},
	}
	findings := []analysis.Finding{{ID: "overlap:alpha:beta", Type: analysis.FindingOverlap, Title: "alpha / beta", Skills: skills[:2]}}
	m := NewWithActions(skills, findings, service)
	updated, _ := m.Update(key("m"))
	m = updated.(Model)
	view := m.View()
	if m.State != StateSelectDraftSkills || !strings.Contains(view, "Overlap group from current finding") || !strings.Contains(view, "Other skills") {
		t.Fatalf("expected grouped overlap picker:\n%s", view)
	}
	if !m.DraftSelections[0] || !m.DraftSelections[1] || m.DraftSelections[2] {
		t.Fatalf("expected alpha/beta preselected, selections=%v", m.DraftSelections)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateGeneratingDraft || cmd == nil {
		t.Fatalf("expected async draft generation, state=%v cmd nil=%v", m.State, cmd == nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.State != StatePreviewDraft || strings.Join(service.draftSelected, ",") != "alpha,beta" {
		t.Fatalf("state=%v selected=%v", m.State, service.draftSelected)
	}
}

func TestDashboardDraftMergeFailureShowsFriendlyAdvisory(t *testing.T) {
	service := &fakeActionService{draftErr: fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY is required")}
	skills := []inventory.Skill{{Name: "alpha"}, {Name: "beta"}}
	m := NewWithActions(skills, nil, service)
	m.Mode = ViewSkills
	updated, _ := m.Update(key("m"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateGeneratingDraft || cmd == nil {
		t.Fatalf("expected async draft generation, state=%v cmd nil=%v", m.State, cmd == nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.State != StateNormal || !strings.Contains(m.Status, "merge draft unavailable") || !strings.Contains(m.Status, "GEMINI_API_KEY") {
		t.Fatalf("expected advisory status, state=%v status=%q", m.State, m.Status)
	}
}

func TestDraftMergePreviewDoesNotMutateInventory(t *testing.T) {
	service := &fakeActionService{}
	skills := []inventory.Skill{{Name: "alpha"}, {Name: "beta"}}
	m := NewWithActions(skills, nil, service)
	m.Mode = ViewSkills
	updated, _ := m.Update(key("m"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	updated, _ = m.Update(key(" "))
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateGeneratingDraft || cmd == nil {
		t.Fatalf("expected async draft generation, state=%v cmd nil=%v", m.State, cmd == nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if len(m.Skills) != 2 || len(service.quarantined) != 0 || len(service.deleted) != 0 || len(service.renamed) != 0 {
		t.Fatalf("draft preview mutated state: skills=%d quarantined=%v deleted=%v renamed=%v", len(m.Skills), service.quarantined, service.deleted, service.renamed)
	}
}

func servicePendingDeletedSkills(service *fakeActionService) []inventory.Skill {
	skills := make([]inventory.Skill, 0, len(service.deletedRoot))
	for i, root := range service.deletedRoot {
		skills = append(skills, inventory.Skill{Name: service.deleted[i], Root: root})
	}
	return skills
}

func testModel(service *fakeActionService) Model {
	skill := inventory.Skill{Name: "alpha", Root: "/root", EncounteredPath: "/root/alpha", PrimaryPath: "/root/alpha/SKILL.md"}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "Duplicate alpha", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{skill}}
	return NewWithActions([]inventory.Skill{skill}, []analysis.Finding{finding}, service)
}

func key(value string) tea.KeyMsg {
	switch value {
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}
	case "ctrl+q":
		return tea.KeyMsg{Type: tea.KeyCtrlQ}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}
