package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
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
func (f *fakeActionService) QuarantineSelected(skills []inventory.Skill) (fsactions.Result, error) {
	result := fsactions.Result{Skills: append([]inventory.Skill(nil), skills...)}
	for _, skill := range skills {
		f.quarantined = append(f.quarantined, skill.Name)
		f.quarantinedRoot = append(f.quarantinedRoot, skill.Root)
		result.Paths = append(result.Paths, "/quarantine/"+skill.Name)
	}
	return result, nil
}
func (f *fakeActionService) DeleteSelected(skills []inventory.Skill, confirmation fsactions.DeleteConfirmation) (fsactions.Result, error) {
	f.deleteTypedName = confirmation.TypedName
	f.deleteBatchToken = confirmation.BatchToken
	result := fsactions.Result{Skills: append([]inventory.Skill(nil), skills...)}
	for _, skill := range skills {
		f.deleted = append(f.deleted, skill.Name)
		f.deletedRoot = append(f.deletedRoot, skill.Root)
		result.Paths = append(result.Paths, skill.EncounteredPath)
	}
	return result, nil
}
func (f *fakeActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (f *fakeActionService) Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error) {
	f.renamed = append(f.renamed, skill.Name+":"+newName)
	return fsactions.PreviewRename(skill, newName), nil
}
func (f *fakeActionService) QuarantinedSkills() ([]string, error) {
	return append([]string(nil), f.quarantinedList...), nil
}
func (f *fakeActionService) Restore(name string, destRoot string) (string, error) {
	f.restored = append(f.restored, name+":"+destRoot)
	return destRoot + "/" + name, nil
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
	if m.State != StateSelectInstall || m.InstallCursor != 1 {
		t.Fatalf("expected action chooser to focus detail-selected install, state=%v cursor=%d", m.State, m.InstallCursor)
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

func TestDashboardDeleteUsesModalConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	if m.State != StateConfirmDelete || !strings.Contains(m.View(), "y confirm") || strings.Contains(m.View(), "type") {
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
