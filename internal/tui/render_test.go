package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestFindingsRenderGroupedAndConsolidatedAtSmallWidth(t *testing.T) {
	findings := []analysis.Finding{
		{ID: "tokens:calendar", Type: analysis.FindingHighTokenCost, Title: "macos-calendar", Skills: []inventory.Skill{{Name: "macos-calendar", LowerTokens: 4100, UpperTokens: 7800}, {Name: "macos-calendar", LowerTokens: 4200, UpperTokens: 7600}}},
		{ID: "activation:calendar", Type: analysis.FindingBroadActivation, Title: "macos-calendar", Skills: []inventory.Skill{{Name: "macos-calendar", ActivationRisk: "high"}, {Name: "macos-calendar", ActivationRisk: "high"}}},
	}
	m := New(nil, findings)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "High token cost") || !strings.Contains(view, "Broad activation risk") {
		t.Fatalf("missing grouped sections:\n%s", view)
	}
	if strings.Count(view, "macos-calendar") > 5 {
		t.Fatalf("same-name finding appears too often:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 92 {
			t.Fatalf("line overflow width=%d line=%q\n%s", lipgloss.Width(line), line, view)
		}
	}
}

func TestFindingListScrollsToSelectedGroup(t *testing.T) {
	var findings []analysis.Finding
	for i := 0; i < 20; i++ {
		name := "skill-" + string(rune('a'+i))
		findings = append(findings, analysis.Finding{ID: name, Type: analysis.FindingHighTokenCost, Title: name, Skills: []inventory.Skill{{Name: name, LowerTokens: 3000, UpperTokens: 5000}}})
	}
	m := New(nil, findings)
	m.Cursor = 19
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "skill-t") || !strings.Contains(view, "… above") {
		t.Fatalf("selected item not visible in scrolled list:\n%s", view)
	}
}

func TestSkillInventoryGroupsDuplicateInstalls(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "alpha", Root: "/one", LowerTokens: 1000, UpperTokens: 3000},
		{Name: "alpha", Root: "/two", LowerTokens: 1200, UpperTokens: 3200},
		{Name: "beta", Root: "/one", LowerTokens: 100, UpperTokens: 200},
	}
	m := New(skills, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	view := m.View()
	if strings.Count(view, "alpha") > 2 || !strings.Contains(view, "2 installs") {
		t.Fatalf("skill inventory did not consolidate installs:\n%s", view)
	}
}

func TestKeybarTruncatesAtSmallWidth(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha"}}, []analysis.Finding{{ID: "x", Type: analysis.FindingDuplicate, Title: "Duplicate alpha", Skills: []inventory.Skill{{Name: "alpha"}}}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m = updated.(Model)
	view := m.View()
	lines := strings.Split(view, "\n")
	keybar := lines[len(lines)-1]
	if lipgloss.Width(keybar) > 72 {
		t.Fatalf("keybar overflow width=%d line=%q", lipgloss.Width(keybar), keybar)
	}
	if !strings.Contains(keybar, "…") {
		t.Fatalf("expected keybar to show hidden actions with ellipsis: %q", keybar)
	}
}
