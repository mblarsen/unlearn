package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartupWarningsRemainReachableInDashboard(t *testing.T) {
	warning := "LLM analysis did not complete: " + strings.Repeat("partial response ", 100) + "LAST-DIAGNOSTIC"
	m := NewWithActionsAndCoverage(nil, nil, nil, "incomplete").WithStartupWarnings([]string{warning})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	if !m.StatusError || !strings.Contains(m.View(), "Audit needs attention") {
		t.Fatal("startup warning not visible")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateFeedback {
		t.Fatal("warning details not reachable")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if !strings.Contains(m.View(), "LAST-DIAGNOSTIC") {
		t.Fatal("full diagnostic not reachable")
	}
	if len(strings.Split(m.View(), "\n")) > 24 {
		t.Fatal("warning overflows frame")
	}
}
