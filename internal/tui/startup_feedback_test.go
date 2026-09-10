package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartupNoticesAreNotPresentedAsErrors(t *testing.T) {
	m := NewWithActionsAndCoverage(nil, nil, nil, "incomplete").WithStartupNotices([]string{"Gemini credentials are not set; using deterministic analysis."})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := m.View()
	if m.StatusError || !strings.Contains(view, "Audit notice") {
		t.Fatalf("startup notice rendered as an error:\n%s", view)
	}
	if strings.Contains(view, "Error") {
		t.Fatalf("startup notice contains error label:\n%s", view)
	}
}

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
