package setup

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/config"
)

func TestSetupModelTogglesAndPersistsConfig(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: true}}, []string{"/sessions/a.jsonl"}, config.Default())
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
		t.Fatalf("history paths=%v", cfg.HistoryJSONL)
	}
}

func TestSetupViewDocumentsBoundedHistoryDiscovery(t *testing.T) {
	m := New([]RootChoice{{Path: "/skills", Exists: false}}, nil, config.Default())
	view := m.View()
	if !strings.Contains(view, "none discovered") || !strings.Contains(view, "miss") {
		t.Fatalf("view missing setup details:\n%s", view)
	}
}
