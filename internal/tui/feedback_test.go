package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestActionErrorRemainsReadableUntilDismissed(t *testing.T) {
	service := &fakeActionService{
		writeRoots: map[string]bool{"/root": true},
		deleteErr:  errors.New("permission denied while removing the selected install because its parent directory is read-only"),
	}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)

	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		updated, _ = m.Update(size)
		m = updated.(Model)
		view := m.View()
		readable := strings.Join(strings.Fields(view), " ")
		for _, want := range []string{"Error", "permission denied while removing", "parent directory is read-only", "Review the error, then retry the action.", "x dismiss"} {
			if !strings.Contains(readable, want) {
				t.Fatalf("%dx%d feedback missing %q:\n%s", size.Width, size.Height, want, view)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("%dx%d feedback overflows: width=%d line=%q", size.Width, size.Height, lipgloss.Width(line), line)
			}
		}
	}

	updated, _ = m.Update(key("x"))
	m = updated.(Model)
	if strings.Contains(m.View(), "permission denied while removing") {
		t.Fatalf("x should dismiss persistent feedback:\n%s", m.View())
	}
}

func TestLongMultilineErrorStaysBoundedAndEverySegmentIsReachable(t *testing.T) {
	segments := make([]string, 120)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment-%03d", i)
	}
	message := "error: Gemini HTTP 500\nresponse body: " + strings.Join(segments, " ")
	m := testModel(&fakeActionService{})
	m.setStatus(message)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	assertViewBounds(t, m.View(), 80, 24)
	if !strings.Contains(m.View(), "enter details") {
		t.Fatalf("bounded feedback must link to its complete details:\n%s", m.View())
	}

	updated, _ = m.Update(key("enter"))
	m = updated.(Model)
	if m.State != StateFeedback {
		t.Fatalf("enter should open feedback details, state=%v", m.State)
	}
	var visited strings.Builder
	for range 160 {
		view := m.View()
		assertViewBounds(t, view, 80, 24)
		visited.WriteString(view)
		visited.WriteByte('\n')
		updated, _ = m.Update(key("j"))
		m = updated.(Model)
	}
	allViews := visited.String()
	for _, want := range append([]string{"Gemini HTTP 500", "response body:"}, segments...) {
		if !strings.Contains(allViews, want) {
			t.Fatalf("feedback detail never exposed %q", want)
		}
	}

	updated, _ = m.Update(key("end"))
	m = updated.(Model)
	if !strings.Contains(m.View(), "segment-119") {
		t.Fatalf("end should expose the last feedback segment:\n%s", m.View())
	}
	updated, _ = m.Update(key("x"))
	m = updated.(Model)
	if m.State != StateNormal || m.Status != "" {
		t.Fatalf("x should dismiss feedback details, state=%v status=%q", m.State, m.Status)
	}
}

func TestActionStatusRemainsVisibleUntilDismissed(t *testing.T) {
	m := testModel(&fakeActionService{})
	updated, _ := m.Update(key("ctrl+k"))
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"Status", "kept alpha", "x dismiss"} {
		if !strings.Contains(view, want) {
			t.Fatalf("status missing %q:\n%s", want, view)
		}
	}
	updated, _ = m.Update(key("j"))
	m = updated.(Model)
	if !strings.Contains(m.View(), "kept alpha") {
		t.Fatalf("status should persist across navigation:\n%s", m.View())
	}
}

func TestNormalFooterReservesHelpAndQuitAndPrioritizesCleanup(t *testing.T) {
	m := testModel(&fakeActionService{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	keybar := lastLine(m.View())
	for _, want := range []string{"? help", "q quit", "ctrl+q quarantine", "ctrl+d delete"} {
		if !strings.Contains(keybar, want) {
			t.Fatalf("80-column keybar missing %q: %q", want, keybar)
		}
	}
	if strings.Contains(keybar, "m draft merge") {
		t.Fatalf("draft merge should not displace cleanup controls: %q", keybar)
	}
}

func TestQuestionMarkShowsContextualHelp(t *testing.T) {
	m := testModel(&fakeActionService{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	updated, _ = m.Update(key("?"))
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"FINDINGS HELP", "Navigation", "Cleanup", "ctrl+d", "delete", "m", "draft merge", "esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("contextual help missing %q:\n%s", want, view)
		}
	}
	updated, _ = m.Update(key("esc"))
	if updated.(Model).State != StateNormal {
		t.Fatalf("escape should close help")
	}
}

func TestConfirmationLabelsRepeatTheirConsequences(t *testing.T) {
	service := &fakeActionService{writeRoots: map[string]bool{"/root": true}}
	m := testModel(service)
	updated, _ := m.Update(key("ctrl+d"))
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"DELETE INSTALL PERMANENTLY", "Permanently delete this exact install?", "y delete", "n cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("delete confirmation missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "y confirm") || strings.Contains(view, "CONFIRM ACTION") {
		t.Fatalf("delete confirmation uses generic labels:\n%s", view)
	}
}

func TestEmptyStatesExplainFindingsAndInventoryNextSteps(t *testing.T) {
	m := New([]inventory.Skill{{Name: "alpha"}}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	findings := m.View()
	for _, want := range []string{"No cleanup findings", "s skills"} {
		if !strings.Contains(findings, want) {
			t.Fatalf("findings empty state missing %q:\n%s", want, findings)
		}
	}

	m = New(nil, nil)
	updated, _ = m.Update(key("s"))
	m = updated.(Model)
	inventoryView := m.View()
	for _, want := range []string{"No skills found", "unlearn scan"} {
		if !strings.Contains(inventoryView, want) {
			t.Fatalf("inventory empty state missing %q:\n%s", want, inventoryView)
		}
	}
}

func assertViewBounds(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("view height=%d exceeds terminal height=%d:\n%s", len(lines), height, view)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			t.Fatalf("view width=%d exceeds terminal width=%d: %q", lipgloss.Width(line), width, line)
		}
	}
}

func lastLine(value string) string {
	lines := strings.Split(value, "\n")
	return lines[len(lines)-1]
}
