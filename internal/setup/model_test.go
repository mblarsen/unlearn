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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
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

func TestSetupKeybarOmitsOptionHotkeys(t *testing.T) {
	m := New(nil, nil, nil, config.Default(), nil)
	view := m.View()

	if strings.Contains(view, "l llm") || strings.Contains(view, "h history") {
		t.Fatalf("setup keybar should not show removed option hotkeys:\n%s", view)
	}
	if !strings.Contains(view, "space toggle") || !strings.Contains(view, "j/k move") {
		t.Fatalf("setup keybar should keep navigation shortcuts:\n%s", view)
	}
}

func TestSetupOptionLetterKeysDoNotToggle(t *testing.T) {
	m := New(nil, nil, nil, config.Default(), nil)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)

	if m.LLMEnabled || m.HistoryScan {
		t.Fatalf("l/h should no longer toggle setup options: llm=%v history=%v", m.LLMEnabled, m.HistoryScan)
	}
}

func TestSetupViewPrioritizesRootsAndOptions(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: false}}, nil, nil, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
	view := m.View()

	rootsIndex := strings.Index(view, "Skill roots")
	optionsIndex := strings.Index(view, "Options")
	agentsIndex := strings.Index(view, "Agent harnesses")
	if rootsIndex == -1 || optionsIndex == -1 || agentsIndex == -1 {
		t.Fatalf("view missing setup sections:\n%s", view)
	}
	if !(rootsIndex < optionsIndex && optionsIndex < agentsIndex) {
		t.Fatalf("setup should show roots and options before agent harnesses:\n%s", view)
	}
}

func TestSetupViewDocumentsBoundedHistoryDiscovery(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: false}}, nil, nil, config.Default(), []inventory.AgentStatus{{AgentDefinition: inventory.AgentDefinition{ID: "pi", DisplayName: "Pi", ShowInSetup: true}, Installed: true}})
	view := m.View()
	if !strings.Contains(view, "none discovered") || !strings.Contains(view, "missing") {
		t.Fatalf("view missing setup details:\n%s", view)
	}
}

func TestSetupScrollKeepsViewportBufferAroundCursor(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: true}}, nil, nil, config.Default(), setupAgentStatuses(18))
	m.Width = 90
	m.Height = 18
	m.Cursor = 14
	m.ensureCursorVisible()

	itemLines := m.setupItemLines()
	buffer := setupScrollBuffer(m.viewportHeight())
	linesBelow := m.ScrollTop + m.viewportHeight() - 1 - itemLines[m.Cursor]
	if linesBelow < buffer {
		t.Fatalf("scrolling down should keep a buffer below the cursor: below=%d buffer=%d scrollTop=%d cursor=%d", linesBelow, buffer, m.ScrollTop, m.Cursor)
	}

	m.Cursor = 5
	m.ensureCursorVisible()
	linesAbove := itemLines[m.Cursor] - m.ScrollTop
	if linesAbove < buffer && m.ScrollTop != 0 {
		t.Fatalf("scrolling up should keep a buffer above the cursor: above=%d buffer=%d scrollTop=%d cursor=%d", linesAbove, buffer, m.ScrollTop, m.Cursor)
	}

	m.Cursor = 0
	m.ensureCursorVisible()
	if !strings.Contains(m.View(), "unlearn setup") {
		t.Fatalf("scrolling back up should reveal the top of setup:\n%s", m.View())
	}
}

func TestSetupViewScrollsToSelectedHarness(t *testing.T) {
	m := New(nil, nil, nil, config.Default(), setupAgentStatuses(18))
	m.Width = 90
	m.Height = 18
	m.Cursor = 17
	m.ensureCursorVisible()
	view := m.View()
	if !strings.Contains(view, "Agent P") || !strings.Contains(view, "… above") {
		t.Fatalf("view should scroll to selected harness:\n%s", view)
	}
	if strings.Contains(view, "Agent A") {
		t.Fatalf("view should not stay pinned to first harness after scrolling:\n%s", view)
	}
}

func setupAgentStatuses(count int) []inventory.AgentStatus {
	statuses := make([]inventory.AgentStatus, 0, count)
	for i := range count {
		statuses = append(statuses, inventory.AgentStatus{AgentDefinition: inventory.AgentDefinition{ID: "agent", DisplayName: "Agent", ShowInSetup: true}})
		statuses[i].ID = "agent-" + string(rune('a'+i))
		statuses[i].DisplayName = "Agent " + string(rune('A'+i))
	}
	return statuses
}
