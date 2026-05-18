package setup

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestSetupModelTogglesAndPersistsConfig(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: true}}, []string{"/sessions/a.jsonl"}, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	cfg := m.ApplyTo(config.Default())
	if !cfg.SetupComplete || !cfg.IsTrusted("/skills") || !cfg.LLMAssisted || !cfg.HistoryScan {
		t.Fatalf("config not persisted from setup: %#v", cfg)
	}
	if len(cfg.HistoryJSONL) != 1 || cfg.HistoryJSONL[0] != "/sessions/a.jsonl" {
		t.Fatalf("history paths=%v", cfg.HistoryJSONL)
	}
	if len(cfg.ActiveAgents) != 1 || cfg.ActiveAgents[0] != "pi" {
		t.Fatalf("active agents=%v", cfg.ActiveAgents)
	}
}

func TestSetupViewDocumentsBoundedHistoryDiscovery(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: false}}, nil, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
	view := m.View()
	if !strings.Contains(view, "none discovered") || !strings.Contains(view, "miss") {
		t.Fatalf("view missing setup details:\n%s", view)
	}
}
