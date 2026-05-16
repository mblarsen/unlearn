package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestModelTogglesViewDensityAndKeys(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha", Kind: inventory.KindDirectory}}, []analysis.Finding{{Title: "Duplicate alpha", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{{Name: "alpha"}}}})
	if m.Mode != ViewFindings || m.Density != DensityCompact {
		t.Fatalf("unexpected initial model %#v", m)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.Mode != ViewSkills {
		t.Fatalf("mode=%v", m.Mode)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if m.Density != DensityRich {
		t.Fatalf("density=%v", m.Density)
	}
	view := m.View()
	if strings.Contains(view, "s skill inventory") || !strings.Contains(view, "f findings") {
		t.Fatalf("dynamic key bar wrong: %s", view)
	}
}
