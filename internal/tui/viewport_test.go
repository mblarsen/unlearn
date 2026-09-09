package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestInstallPickerKeepsCursorAndOptionsVisibleForLongLists(t *testing.T) {
	var skills []inventory.Skill
	for i := 0; i < 30; i++ {
		skillPath := fmt.Sprintf("/tmp/fixtures/root-%02d/skills/skill-%02d", i, i)
		skills = append(skills, inventory.Skill{Name: fmt.Sprintf("skill-%02d", i), Root: fmt.Sprintf("/tmp/fixtures/root-%02d", i), EncounteredPath: skillPath})
	}
	m := New(skills, []analysis.Finding{{ID: "duplicate:many", Type: analysis.FindingDuplicate, Title: "many", Skills: skills}})
	m.State = StateSelectInstall
	m.PendingAction = ActionDelete
	m.PendingFinding = m.Findings[0]
	m.Width, m.Height = 80, 24
	for range len(skills) - 1 {
		updated, _ := m.Update(key("j"))
		m = updated.(Model)
	}

	view := m.View()
	for _, want := range []string{"skill-29", "/skills/skill-29", "Options"} {
		if !strings.Contains(view, want) {
			t.Fatalf("long install picker hid %q:\n%s", want, view)
		}
	}
}

func TestInstallPickerCanScrollWithinASelectedLongPath(t *testing.T) {
	var skills []inventory.Skill
	for i := 0; i < 30; i++ {
		path := fmt.Sprintf("/tmp/root-%02d/", i)
		if i == 15 {
			path += strings.Repeat("nested-directory/", 12) + "EXACT-TARGET"
		}
		skills = append(skills, inventory.Skill{Name: fmt.Sprintf("skill-%02d", i), EncounteredPath: path})
	}
	for _, height := range []int{18, 24} {
		t.Run(fmt.Sprintf("80x%d", height), func(t *testing.T) {
			m := New(skills, []analysis.Finding{{ID: "duplicate:many", Type: analysis.FindingDuplicate, Title: "many", Skills: skills}})
			m.State = StateSelectInstall
			m.PendingAction = ActionDelete
			m.PendingFinding = m.Findings[0]
			m.InstallCursor = 15
			m.Width, m.Height = 80, height

			for range 10 {
				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
				m = updated.(Model)
			}
			view := m.View()
			if !strings.Contains(view, "EXACT-TARGET") || !strings.Contains(view, "Options") {
				t.Fatalf("selected long path suffix is not reachable with page-down:\n%s", view)
			}
			for range 10 {
				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
				m = updated.(Model)
			}
			if view := m.View(); !strings.Contains(view, "skill-15") {
				t.Fatalf("selected long path start is not reachable with page-up:\n%s", view)
			}
		})
	}
}

func TestRestoreAndBatchPickersFollowCursor(t *testing.T) {
	m := New(nil, nil)
	m.Width, m.Height = 80, 24
	for i := 0; i < 30; i++ {
		m.RestoreChoices = append(m.RestoreChoices, fmt.Sprintf("quarantined-skill-%02d", i))
		m.BatchRootChoices = append(m.BatchRootChoices, fsactions.BatchRootChoice{Root: fmt.Sprintf("/tmp/fixtures/very-long-root-%02d", i), Skills: []inventory.Skill{{Name: "alpha"}}})
	}

	m.State = StateSelectRestore
	for range 29 {
		updated, _ := m.Update(key("j"))
		m = updated.(Model)
	}
	if view := m.View(); !strings.Contains(view, "quarantined-skill-29") || !strings.Contains(view, "Options") {
		t.Fatalf("restore picker did not follow cursor:\n%s", view)
	}
	m.State, m.BatchRootCursor = StateSelectBatchRoot, 0
	for range 29 {
		updated, _ := m.Update(key("j"))
		m = updated.(Model)
	}
	if view := m.View(); !strings.Contains(view, "very-long-root-29") || !strings.Contains(view, "Options") {
		t.Fatalf("batch picker did not follow cursor:\n%s", view)
	}
}

func TestLongConfirmationKeepsOptionsVisible(t *testing.T) {
	m := New(nil, nil)
	m.Width, m.Height = 80, 24
	m.State = StateConfirmDelete
	m.Message = "Delete selected installs permanently?\n" + strings.Repeat("/tmp/fixtures/a-very-long-install-path/skill\n", 30)

	if view := m.View(); !strings.Contains(view, "Options") {
		t.Fatalf("long confirmation hid its controls:\n%s", view)
	}
}

func TestSelectedInstallDetailWrapsCompletePath(t *testing.T) {
	path := "/tmp/fixtures/a-very-long-root-name/with/many/nested/directories/that-must-remain-visible/final-skill"
	skill := inventory.Skill{Name: "alpha", Root: "/tmp/fixtures/a-very-long-root-name", EncounteredPath: path}
	m := New([]inventory.Skill{skill}, []analysis.Finding{{ID: "duplicate:alpha", Type: analysis.FindingDuplicate, Title: "alpha", Skills: []inventory.Skill{skill}}})
	m.Density = DensityRich
	m.Width, m.Height = 120, 40

	view := m.View()
	if !strings.Contains(view, "final-skill") {
		t.Fatalf("selected detail permanently truncated path:\n%s", view)
	}
}

func TestUnsupportedViewportShowsResizeGateWithoutOverflow(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 24}, {Width: 80, Height: 17}} {
		m := New(nil, nil)
		updated, _ := m.Update(size)
		m = updated.(Model)
		view := m.View()
		if !strings.Contains(strings.ToLower(view), "terminal too small") || !strings.Contains(view, "80×18") {
			t.Fatalf("small viewport should explain minimum size at %dx%d:\n%s", size.Width, size.Height, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("small viewport overflowed at %dx%d: %q", size.Width, size.Height, line)
			}
		}
	}
}

func TestSupportedViewportMatrixDoesNotOverflow(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			m := New([]inventory.Skill{{Name: "alpha", Root: "/tmp/fixtures/root"}}, nil)
			updated, _ := m.Update(size)
			view := updated.(Model).View()
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size.Width {
					t.Fatalf("viewport overflowed at %dx%d: %q", size.Width, size.Height, line)
				}
			}
		})
	}
}
