package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestDashboardDiscoveryQueryResultsInspectAndBack(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "browser-test", Description: "Test browser interfaces", EncounteredPath: "/skills/pi/browser-test", Root: "/skills/pi", RootKnown: true, ActiveAgents: []string{"pi"}, HistoryEvidence: "medium"},
		{Name: "web-check", Description: "Check browser accessibility", EncounteredPath: "/skills/codex/web-check", Root: "/skills/codex", RootKnown: true, ActiveAgents: []string{"codex"}},
	}
	findings := []analysis.Finding{{ID: "overlap:browser", Type: analysis.FindingOverlap, Skills: skills}}
	m := New(skills, findings)
	m.Width, m.Height = 100, 30

	updated, _ := m.Update(key("d"))
	m = updated.(Model)
	if m.State != StateDiscoveryQuery || !strings.Contains(m.View(), "WHAT HELPS ME DO THIS?") {
		t.Fatalf("discovery query did not open:\n%s", m.View())
	}
	updated, _ = m.Update(key("test browser"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"DISCOVERY RESULTS", "browser-test", "web-check", "2 matching installed skills"} {
		if !strings.Contains(view, want) {
			t.Fatalf("results missing %q:\n%s", want, view)
		}
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view = m.View()
	for _, want := range []string{"SKILL MATCH", "name matches", "description matches", "/skills/pi/browser-test", "Accessible to: pi", "Invocation: observed: medium", "Known overlap in these results: web-check"} {
		if !strings.Contains(view, want) {
			t.Fatalf("inspection missing %q:\n%s", want, view)
		}
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.State != StateDiscoveryResults {
		t.Fatalf("inspect back state=%v", m.State)
	}
	updated, _ = m.Update(key("e"))
	m = updated.(Model)
	if m.State != StateDiscoveryQuery || !strings.Contains(m.View(), "test browser") {
		t.Fatalf("edit did not preserve query:\n%s", m.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.State != StateNormal {
		t.Fatalf("query back state=%v", m.State)
	}
}

func TestDashboardDiscoveryAcceptsRealTerminalSpaceKeys(t *testing.T) {
	m := New([]inventory.Skill{{Name: "browser-test", Description: "Test browser accessibility", EncounteredPath: "/skills/browser"}}, nil)
	updated, _ := m.Update(key("d"))
	m = updated.(Model)
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("test")},
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune("browser")},
	} {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}
	if m.Discovery.Query != "test browser" {
		t.Fatalf("query=%q, want real terminal spaces preserved", m.Discovery.Query)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if len(m.Discovery.Result.Matches) != 1 {
		t.Fatalf("spaced query did not find skill: %#v", m.Discovery.Result)
	}
}

func TestDashboardDiscoveryShowsWeakAndNoMatchStates(t *testing.T) {
	m := New([]inventory.Skill{{Name: "notes", Description: "Create local notes"}}, nil)
	m.Width, m.Height = 80, 24
	for _, query := range []string{"help with task", "deploy kubernetes"} {
		updated, _ := m.Update(key("d"))
		m = updated.(Model)
		updated, _ = m.Update(key(query))
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if m.State != StateDiscoveryResults {
			t.Fatalf("query %q state=%v", query, m.State)
		}
		view := m.View()
		if query == "help with task" && !strings.Contains(view, "Use more specific terms") {
			t.Fatalf("weak state missing guidance:\n%s", view)
		}
		if query == "deploy kubernetes" && !strings.Contains(view, "No observed matching installed skill") {
			t.Fatalf("empty state overclaims or lacks explanation:\n%s", view)
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = updated.(Model)
	}
}

func TestDashboardDiscoveryKeepsLongResultsAndInstallPathsReachable(t *testing.T) {
	var skills []inventory.Skill
	longAgent := "agent-" + strings.Repeat("long-segment-", 12) + "tail"
	for i := 0; i < 25; i++ {
		skill := inventory.Skill{
			Name:            fmt.Sprintf("browser-helper-%02d", i),
			Description:     "Test browser accessibility",
			EncounteredPath: fmt.Sprintf("/tmp/root/%s/final-skill-%02d", strings.Repeat("long-segment/", 12), i),
		}
		if i == 24 {
			skill.Description = "Test browser accessibility " + strings.Repeat("abcdefgh ", 600)
			skill.RootKnown = true
			skill.ActiveAgents = []string{longAgent}
		}
		skills = append(skills, skill)
	}
	m := New(skills, nil)
	m.Width, m.Height = 80, 18
	updated, _ := m.Update(key("d"))
	m = updated.(Model)
	updated, _ = m.Update(key("browser accessibility"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	for range 24 {
		updated, _ = m.Update(key("j"))
		m = updated.(Model)
	}
	if view := m.View(); !strings.Contains(view, "browser-helper-24") {
		t.Fatalf("last result is not reachable:\n%s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	seenPathEnd := false
	seenAgentEnd := false
	for range 250 {
		view := m.View()
		assertViewportBounds(t, view, 80, 18)
		seenPathEnd = seenPathEnd || strings.Contains(view, "final-skill-24")
		seenAgentEnd = seenAgentEnd || strings.Contains(view, "tail")
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = updated.(Model)
	}
	if !seenPathEnd || !seenAgentEnd {
		t.Fatalf("variable fact tails are not reachable (path=%t agent=%t):\n%s", seenPathEnd, seenAgentEnd, m.View())
	}
}

func TestDashboardDiscoveryHelpAndViewportMatrix(t *testing.T) {
	m := New([]inventory.Skill{{Name: "résumé-check", Description: "Check résumé wording", EncounteredPath: "/skills/résumé-check"}}, nil)
	m.Width, m.Height = 100, 30
	updated, _ := m.Update(key("?"))
	m = updated.(Model)
	if !strings.Contains(m.View(), "d discover by task") {
		t.Fatalf("help does not expose discovery:\n%s", m.View())
	}

	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 18}, {Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			model := New(m.Skills, nil)
			model.Width, model.Height = size.Width, size.Height
			updated, _ := model.Update(key("d"))
			model = updated.(Model)
			updated, _ = model.Update(key("résumé wording"))
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			view := updated.(Model).View()
			assertViewportBounds(t, view, size.Width, size.Height)
		})
	}
}
