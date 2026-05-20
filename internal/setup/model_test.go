package setup

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestSetupModelTogglesAndPersistsConfig(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: true}}, []string{"/sessions/a.jsonl"}, []string{"/sessions/history.db"}, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
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
		t.Fatalf("history JSONL paths=%v", cfg.HistoryJSONL)
	}
	if len(cfg.HistorySQLite) != 1 || cfg.HistorySQLite[0] != "/sessions/history.db" {
		t.Fatalf("history SQLite paths=%v", cfg.HistorySQLite)
	}
	if len(cfg.ActiveAgents) != 1 || cfg.ActiveAgents[0] != "pi" {
		t.Fatalf("active agents=%v", cfg.ActiveAgents)
	}
}

func TestSetupViewDocumentsBoundedHistoryDiscovery(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: false}}, nil, nil, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
	view := m.View()
	if !strings.Contains(view, "none discovered") || !strings.Contains(view, "miss") {
		t.Fatalf("view missing setup details:\n%s", view)
	}
}

func TestSetupViewScrollsToSelectedHarness(t *testing.T) {
	statuses := make([]inventory.AgentStatus, 0, 18)
	for i := range 18 {
		statuses = append(statuses, inventory.AgentStatus{AgentDefinition: inventory.AgentDefinition{ID: "agent", DisplayName: "Agent", ShowInSetup: true}})
		statuses[i].ID = "agent-" + string(rune('a'+i))
		statuses[i].DisplayName = "Agent " + string(rune('A'+i))
	}
	m := New(nil, nil, nil, config.Default(), statuses)
	m.Width = 90
	m.Height = 18
	m.Cursor = 15

	view := m.View()
	if !strings.Contains(view, "Agent P") || !strings.Contains(view, "… above") {
		t.Fatalf("view should scroll to selected harness:\n%s", view)
	}
	if strings.Contains(view, "Agent A") {
		t.Fatalf("view should not stay pinned to first harness after scrolling:\n%s", view)
	}
}
