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
	writeRoots      map[string]bool
	kept            []string
	ignored         []string
	quarantined     []string
	quarantinedRoot []string
	deleted         []string
	renamed         []string
	restored        []string
	deleteTypedName string
}

func (f *fakeActionService) KeepSkill(skill inventory.Skill) error {
	f.kept = append(f.kept, skill.Name)
	return nil
}
func (f *fakeActionService) IgnoreFinding(finding analysis.Finding) error {
	f.ignored = append(f.ignored, finding.ID)
	return nil
}
func (f *fakeActionService) CanWrite(root string) bool { return f.writeRoots[root] }
func (f *fakeActionService) AllowWrite(root string) error {
	if f.writeRoots == nil {
		f.writeRoots = map[string]bool{}
	}
	f.writeRoots[root] = true
	return nil
}
func (f *fakeActionService) Quarantine(skill inventory.Skill) (string, error) {
	f.quarantined = append(f.quarantined, skill.Name)
	f.quarantinedRoot = append(f.quarantinedRoot, skill.Root)
	return "/quarantine/" + skill.Name, nil
}
func (f *fakeActionService) Delete(skill inventory.Skill, typedName string) error {
	f.deleteTypedName = typedName
	f.deleted = append(f.deleted, skill.Name)
	return nil
}
func (f *fakeActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (f *fakeActionService) Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error) {
	f.renamed = append(f.renamed, skill.Name+":"+newName)
	return fsactions.PreviewRename(skill, newName), nil
}
func (f *fakeActionService) Restore(name string, destRoot string) (string, error) {
	f.restored = append(f.restored, name+":"+destRoot)
	return destRoot + "/" + name, nil
}

func TestDashboardKeepAndIgnoreFindingActions(t *testing.T) {
	service := &fakeActionService{}
	m := testModel(service)
	updated, _ := m.Update(key("K"))
	m = updated.(Model)
	if len(service.kept) != 1 || service.kept[0] != "alpha" {
		t.Fatalf("kept=%v", service.kept)
	}
	updated, _ = m.Update(key("I"))
	m = updated.(Model)
	if len(service.ignored) != 1 || service.ignored[0] != "duplicate:alpha" {
		t.Fatalf("ignored=%v", service.ignored)
	}
}

func TestDashboardQuarantineRequiresWriteGateAndConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{}}
	m := testModel(service)
	updated, _ := m.Update(key("Q"))
	m = updated.(Model)
	if m.State != StateWriteGate || !strings.Contains(m.View(), "this exact install") || !strings.Contains(m.View(), "/root/alpha") {
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

func TestDashboardRequiresInstallChoiceForDuplicateFindingActions(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/two": true}}
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", EncounteredPath: "/one/alpha", PrimaryPath: "/one/alpha/SKILL.md"},
		{Name: "alpha", Root: "/two", EncounteredPath: "/two/alpha", PrimaryPath: "/two/alpha/SKILL.md"},
		{Name: "alpha", Root: "/three", EncounteredPath: "/three/alpha", PrimaryPath: "/three/alpha/SKILL.md"},
	}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "alpha", Type: analysis.FindingDuplicate, Skills: skills}
	m := NewWithActions(skills, []analysis.Finding{finding}, service)
	updated, _ := m.Update(key("Q"))
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
}

func TestDashboardDeleteUsesModalConfirmation(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("D"))
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
	updated, _ := m.Update(key("N"))
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
	updated, _ := m.Update(key("N"))
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

func TestDashboardRestoreUsesSelectedRoot(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("R"))
	m = updated.(Model)
	for _, r := range "old" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if len(service.restored) != 1 || service.restored[0] != "old:/root" {
		t.Fatalf("restored=%v", service.restored)
	}
}

func testModel(service *fakeActionService) Model {
	skill := inventory.Skill{Name: "alpha", Root: "/root", EncounteredPath: "/root/alpha", PrimaryPath: "/root/alpha/SKILL.md"}
	finding := analysis.Finding{ID: "duplicate:alpha", Title: "Duplicate alpha", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{skill}}
	return NewWithActions([]inventory.Skill{skill}, []analysis.Finding{finding}, service)
}

func key(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}
