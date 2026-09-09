package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestEnterOpensScrollableSkillStoryFromInventory(t *testing.T) {
	seen := time.Date(2026, 5, 19, 14, 30, 0, 0, time.UTC)
	skills := []inventory.Skill{
		{Name: "alpha", EncounteredPath: "/tmp/skills/α-one", ResolvedPath: "/tmp/skills/α-one", Provenance: "pi global skills root", ActiveAgents: []string{"pi"}, Frontmatter: map[string]string{"name": "alpha", "description": "one"}, Body: "one", ContentHash: "111", HistoryEvidence: "strong", HistorySources: []string{"/tmp/pi-history.jsonl"}, HistoryLastSeenAt: seen},
		{Name: "alpha", EncounteredPath: "/tmp/skills/α-two", ResolvedPath: "/tmp/skills/α-two", Provenance: "user-provided root", Frontmatter: map[string]string{"name": "alpha", "description": "two"}, Body: "two", ContentHash: "222"},
	}
	m := NewWithActionsAndCoverage(skills, nil, NoopActionService{}, audit.EvidenceComplete)
	m.Mode = ViewSkills
	m.Width, m.Height = 80, 18

	updated, _ := m.Update(key("enter"))
	m = updated.(Model)
	if m.State != StateSkillStory {
		t.Fatalf("enter did not open story: state=%v", m.State)
	}

	seenText := ""
	for range 50 {
		view := m.View()
		assertViewportBounds(t, view, 80, 18)
		seenText += "\n" + view
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = updated.(Model)
	}
	for _, want := range []string{
		"SKILL STORY", "α-one", "α-two", "Origin", "unknown", "Install date",
		"Upstream baseline", "modification requires", "pi global skills root",
		"active harness access: pi", "metadata description differs", "SKILL.md body differs",
		"strong derived evidence", "2026-05-19T14:30:00Z", "skill name, not a",
		"specific installed copy", "Not observed does not mean unused",
	} {
		if !strings.Contains(seenText, want) {
			t.Errorf("scrollable story never showed %q:\n%s", want, seenText)
		}
	}
}

func TestSkillStoryBackReturnsToSelectedInventorySkill(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha", EncounteredPath: "/tmp/alpha"}, {Name: "beta", EncounteredPath: "/tmp/beta"}}, nil)
	m.Mode = ViewSkills
	m.Cursor = 1
	updated, _ := m.Update(key("enter"))
	m = updated.(Model)
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	if m.Cursor != 1 || m.StoryScroll == 0 {
		t.Fatalf("story scroll changed inventory selection: cursor=%d scroll=%d", m.Cursor, m.StoryScroll)
	}
	updated, _ = m.Update(key("esc"))
	m = updated.(Model)
	if m.State != StateNormal || m.Cursor != 1 || m.StoryScroll != 0 {
		t.Fatalf("back did not restore inventory context: %#v", m)
	}
}

func TestEnterDoesNotReplaceFindingOrFeedbackBehavior(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha"}}, nil)
	m.Status = "existing details"
	updated, _ := m.Update(key("enter"))
	if got := updated.(Model).State; got != StateFeedback {
		t.Fatalf("existing feedback behavior changed: %v", got)
	}

	m = New([]inventory.Skill{{Name: "alpha"}}, nil)
	updated, _ = m.Update(key("enter"))
	if got := updated.(Model).State; got != StateNormal {
		t.Fatalf("findings enter should remain unchanged: %v", got)
	}
}

func TestSkillInventoryHelpAndFooterDiscoverStory(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha", EncounteredPath: "/tmp/alpha"}}, nil)
	m.Mode = ViewSkills
	m.Width, m.Height = 120, 40
	if view := m.View(); !strings.Contains(strings.ToLower(view), "enter story") {
		t.Fatalf("inventory footer does not expose story:\n%s", view)
	}
	updated, _ := m.Update(key("?"))
	view := updated.(Model).View()
	if !strings.Contains(strings.ToLower(view), "enter") || !strings.Contains(strings.ToLower(view), "skill story") {
		t.Fatalf("inventory help does not explain story:\n%s", view)
	}
}

func TestSkillStoryViewportMatrix(t *testing.T) {
	var skills []inventory.Skill
	for i := 0; i < 8; i++ {
		skills = append(skills, inventory.Skill{Name: "alpha", EncounteredPath: fmt.Sprintf("/tmp/非常に長い/skill-%02d", i), ContentHash: fmt.Sprintf("%d", i), Body: fmt.Sprintf("body-%d", i)})
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 18}, {Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			m := NewWithActionsAndCoverage(skills, nil, NoopActionService{}, audit.EvidenceIncomplete)
			m.Mode = ViewSkills
			m.Width, m.Height = size.Width, size.Height
			updated, _ := m.Update(key("enter"))
			assertViewportBounds(t, updated.(Model).View(), size.Width, size.Height)
		})
	}
}

func TestSkillStoryPagingDoesNotSkipContentForwardBackwardOrAfterResize(t *testing.T) {
	metadata := make(map[string]string)
	markers := make([]string, 36)
	for i := range markers {
		markers[i] = fmt.Sprintf("marker-%02d", i)
		metadata[markers[i]] = "present"
	}
	m := New([]inventory.Skill{
		{Name: "alpha", EncounteredPath: "/tmp/alpha-a", Frontmatter: metadata},
		{Name: "alpha", EncounteredPath: "/tmp/alpha-b"},
	}, nil)
	m.Mode = ViewSkills
	m.Width, m.Height = 80, 18
	updated, _ := m.Update(key("enter"))
	m = updated.(Model)

	var forward strings.Builder
	for {
		forward.WriteString(m.View())
		before := m.StoryScroll
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = updated.(Model)
		if m.StoryScroll == before {
			break
		}
	}
	assertStoryMarkersSeen(t, forward.String(), markers)

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	assertViewportBounds(t, m.View(), 120, 24)

	var backward strings.Builder
	for {
		backward.WriteString(m.View())
		before := m.StoryScroll
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		m = updated.(Model)
		if m.StoryScroll == before {
			break
		}
	}
	assertStoryMarkersSeen(t, backward.String(), markers)
}

func assertStoryMarkersSeen(t *testing.T, rendered string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if !strings.Contains(rendered, marker) {
			t.Errorf("paged story skipped %q", marker)
		}
	}
}
