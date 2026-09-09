package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/collections"
	"github.com/mblarsen/unlearn/internal/inventory"
)

type collectionActionFake struct {
	NoopActionService
	items    []collections.Collection
	commands []collections.Command
}

func (f *collectionActionFake) ExecuteCollection(command collections.Command, skills []inventory.Skill) collections.Result {
	f.commands = append(f.commands, command)
	result := collections.Execute(f.items, skills, command)
	if result.Err == nil && result.Changed {
		f.items = result.Collections
	}
	return result
}

func TestCollectionsTUILifecycleAndScopedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills", "alpha")
	skills := []inventory.Skill{{Name: "alpha", EncounteredPath: path, Description: "Build accessible frontend interfaces", ActiveAgents: []string{"pi"}}}
	service := &collectionActionFake{}
	m := NewWithActions(skills, nil, service)
	m.Width, m.Height = 80, 24

	m = updateModel(m, key("c"))
	if m.Mode != ViewCollections || !strings.Contains(m.View(), "No collections") {
		t.Fatalf("collections did not open: %s", m.View())
	}
	m = updateModel(m, key("n"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Frontend 🌱")})
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(service.items) != 1 || service.items[0].Name != "Frontend 🌱" {
		t.Fatalf("collections=%#v", service.items)
	}

	m = updateModel(m, key("a"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(service.items[0].Members) != 1 || service.items[0].Members[0].InstallPath != path {
		t.Fatalf("members=%#v", service.items[0].Members)
	}
	view := m.View()
	if !strings.Contains(view, "inventory") || !strings.Contains(view, "evidence") || !strings.Contains(view, "pi") {
		t.Fatalf("availability evidence missing: %s", view)
	}

	// Collection-local s opens suggestion input instead of the global skills view.
	m = updateModel(m, key("s"))
	if m.State != StateCollectionInput || m.Mode != ViewCollections {
		t.Fatalf("scoped suggestion key failed: mode=%v state=%v", m.Mode, m.State)
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode != ViewFindings {
		t.Fatalf("back did not return to findings: %v", m.Mode)
	}
}

func TestCollectionRenameDeleteAndUnicodeInputDoNotTouchSkillFiles(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "alpha")
	if err := os.WriteFile(skillPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &collectionActionFake{items: []collections.Collection{{Name: "Old"}}}
	m := NewWithActions([]inventory.Skill{{Name: "alpha", EncounteredPath: skillPath}}, nil, service)
	m.Width, m.Height = 100, 30
	m = updateModel(m, key("c"))

	m = updateModel(m, key("r"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter}) // empty input leaves the input active with feedback
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x Research")})
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if service.items[0].Name != "x Research" {
		t.Fatalf("rename did not accept all typed characters: %#v", service.items)
	}

	m = updateModel(m, key("ctrl+d"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if len(service.items) != 1 {
		t.Fatal("cancel deleted collection")
	}
	m = updateModel(m, key("ctrl+d"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if len(service.items) != 0 {
		t.Fatalf("collection not deleted: %#v", service.items)
	}
	content, err := os.ReadFile(skillPath)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("collection deletion touched skill file: content=%q err=%v", content, err)
	}
}

func TestCollectionSuggestionsRequireManualAcceptance(t *testing.T) {
	skill := inventory.Skill{Name: "frontend", EncounteredPath: filepath.Join(t.TempDir(), "frontend"), Description: "Build accessible web interfaces"}
	service := &collectionActionFake{items: []collections.Collection{{Name: "Project"}}}
	m := NewWithActions([]inventory.Skill{skill}, nil, service)
	m.Width, m.Height = 80, 24
	m = updateModel(m, key("c"))
	m = updateModel(m, key("s"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("accessible frontend")})
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(service.items[0].Members) != 0 || m.State != StateCollectionSuggestions {
		t.Fatalf("suggestion auto-accepted: state=%v items=%#v", m.State, service.items)
	}
	if !strings.Contains(m.View(), "manual") {
		t.Fatalf("manual acceptance not explained: %s", m.View())
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(service.items[0].Members) != 1 {
		t.Fatalf("manual acceptance failed: %#v", service.items)
	}
}

func TestCollectionsTUIShowsAndRemovesStaleMembership(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "research")
	service := &collectionActionFake{items: []collections.Collection{{Name: "Research", Members: []collections.Member{{SkillName: "research", InstallPath: missing}}}}}
	m := NewWithActions(nil, nil, service)
	m.Width, m.Height = 120, 40
	m = updateModel(m, key("c"))
	if !strings.Contains(m.View(), "STALE") || !strings.Contains(m.View(), "missing/research") {
		t.Fatalf("stale membership unclear: %s", m.View())
	}
	m = updateModel(m, key("tab"))
	m = updateModel(m, key("x"))
	if len(service.items[0].Members) != 0 {
		t.Fatalf("stale membership not removable: %#v", service.items)
	}
}

func TestCollectionsCoexistWithDiscoveryStoryAndGuidedReviewRoutes(t *testing.T) {
	skill := inventory.Skill{Name: "alpha", EncounteredPath: filepath.Join(t.TempDir(), "alpha"), Description: "Research frontend accessibility"}
	service := &collectionActionFake{}
	m := NewWithActions([]inventory.Skill{skill}, nil, service)

	m = updateModel(m, key("c"))
	if m.Mode != ViewCollections {
		t.Fatalf("c mode=%v", m.Mode)
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, key("d"))
	if m.State != StateDiscoveryQuery {
		t.Fatalf("d state=%v", m.State)
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, key("s"))
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.State != StateSkillStory {
		t.Fatalf("enter state=%v", m.State)
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, key("v"))
	if m.Mode != ViewGuidedReview {
		t.Fatalf("v mode=%v", m.Mode)
	}
}

func TestCollectionsRenderBoundedAndHelpDiscoverable(t *testing.T) {
	service := &collectionActionFake{items: []collections.Collection{{Name: "Research"}}}
	for _, size := range [][2]int{{80, 18}, {80, 24}, {120, 40}, {200, 60}} {
		m := NewWithActions(nil, nil, service)
		m.Width, m.Height = size[0], size[1]
		m = updateModel(m, key("c"))
		if got := len(strings.Split(m.View(), "\n")); got > size[1] {
			t.Fatalf("size=%v rendered %d lines", size, got)
		}
		m = updateModel(m, key("?"))
		view := m.View()
		if !strings.Contains(view, "COLLECTIONS HELP") || !strings.Contains(view, "ctrl+d delete collection") {
			t.Fatalf("size=%v help=%s", size, view)
		}
	}
}

func updateModel(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}
