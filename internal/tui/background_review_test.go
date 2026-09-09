package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
)

func TestBackgroundReviewProgressAndFindingsDoNotChangeInteractionState(t *testing.T) {
	t.Parallel()
	beta := inventory.Skill{ID: "beta", Name: "beta", EncounteredPath: "/root/beta", ContentHash: "b"}
	review := &fakeBackgroundReview{}
	m := New([]inventory.Skill{beta}, nil).WithBackgroundReview(review)
	m.State = StateInputRename
	m.Input = "unfinished"
	m.Cursor = 3
	m.DetailCursor = 2

	progress := audit.Progress{Step: "llm-quality", Current: 1, Total: 2, Detail: "beta"}
	updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Progress: &progress}})
	m = updated.(Model)
	if !strings.Contains(m.View(), "LLM quality 1/2") {
		t.Fatalf("header missing live progress:\n%s", m.View())
	}
	finding := analysis.Finding{ID: "skill-quality:beta", Type: analysis.FindingSkillQuality, Title: "beta", Skills: []inventory.Skill{beta}, Reasons: []string{"advisory"}}
	updated, _ = m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Finding: &finding}})
	m = updated.(Model)

	if m.State != StateInputRename || m.Input != "unfinished" || m.Cursor != 3 || m.DetailCursor != 2 {
		t.Fatalf("background update changed interaction state: %#v", m)
	}
	if len(m.Findings) != 1 || len(review.snapshots) != 1 {
		t.Fatalf("findings=%#v persisted=%d", m.Findings, len(review.snapshots))
	}
}

func TestBackgroundReviewFindingInsertionPreservesSelectedFindingAndInstall(t *testing.T) {
	t.Parallel()
	alpha := inventory.Skill{ID: "alpha", Name: "alpha", EncounteredPath: "/root/alpha", ContentHash: "a"}
	beta := inventory.Skill{ID: "beta", Name: "beta", EncounteredPath: "/root/beta", ContentHash: "b"}
	broken := analysis.Finding{ID: "broken:beta", Type: analysis.FindingBroken, Title: "beta", Skills: []inventory.Skill{alpha, beta}}
	m := New([]inventory.Skill{alpha, beta}, []analysis.Finding{broken}).WithBackgroundReview(&fakeBackgroundReview{})
	m.Mode = ViewFindings
	m.Cursor = 0
	m.DetailCursor = 1
	overlap := analysis.Finding{ID: "llm-overlap:alpha:beta", Type: analysis.FindingOverlap, Title: "alpha / beta", Skills: []inventory.Skill{alpha, beta}}

	updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Finding: &overlap}})
	m = updated.(Model)
	selected, ok := m.selectedFinding()
	if !ok || selected.ID != broken.ID {
		t.Fatalf("selection changed to %#v", selected)
	}
	skill, ok := m.selectedSkill()
	if !ok || skill.ID != beta.ID {
		t.Fatalf("install selection changed to %#v", skill)
	}
}

func TestBackgroundReviewRejectsRemovedOrChangedInstallsBeforePersistence(t *testing.T) {
	t.Parallel()
	current := inventory.Skill{ID: "current-beta", Name: "beta", EncounteredPath: "/root/replacement-beta", ContentHash: "new"}
	staleBeta := inventory.Skill{ID: "old-beta", Name: "beta", EncounteredPath: "/root/beta", ContentHash: "old"}
	removed := inventory.Skill{ID: "alpha", Name: "alpha", EncounteredPath: "/root/alpha", ContentHash: "a"}
	review := &fakeBackgroundReview{}
	m := New([]inventory.Skill{current}, nil).WithBackgroundReview(review)
	finding := analysis.Finding{ID: "llm-overlap:alpha:beta", Type: analysis.FindingOverlap, Title: "alpha / beta", Skills: []inventory.Skill{removed, staleBeta}}
	updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Finding: &finding}})
	m = updated.(Model)
	summary := llm.GeneratedSummary{Name: "beta", ContentHash: "old", Summary: "stale revision", Provider: "test", Model: "fake"}
	updated, _ = m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Summary: &summary, SummarySkills: []inventory.Skill{staleBeta}}})
	m = updated.(Model)
	replacedIdentity := staleBeta
	replacedIdentity.ContentHash = "new"
	summary.ContentHash = "new"
	summary.Summary = "stale identity"
	updated, _ = m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Summary: &summary, SummarySkills: []inventory.Skill{replacedIdentity}}})
	m = updated.(Model)

	if len(m.Findings) != 0 || m.Skills[0].LLMSummary != "" {
		t.Fatalf("stale review result applied: skills=%#v findings=%#v", m.Skills, m.Findings)
	}
	for _, snapshot := range review.snapshots {
		if len(snapshot.Skills) != 1 || snapshot.Skills[0].Name != "beta" || snapshot.Skills[0].ContentHash != "new" {
			t.Fatalf("persisted stale snapshot: %#v", snapshot)
		}
	}
}

func TestBackgroundReviewFailureIsBoundedAndNavigable(t *testing.T) {
	t.Parallel()
	m := New([]inventory.Skill{{Name: "beta"}}, nil).WithBackgroundReview(&fakeBackgroundReview{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m = updated.(Model)
	diagnostic := audit.Diagnostic{Code: audit.DiagnosticLLMFallback, Message: "review failed: " + strings.Repeat("detail ", 100) + "LAST-DIAGNOSTIC"}
	updated, _ = m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Diagnostic: &diagnostic}})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "LLM error") {
		t.Fatalf("header does not expose review failure:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line overflow width=%d line=%q", lipgloss.Width(line), line)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if m.State != StateFeedback || !strings.Contains(m.View(), "LAST-DIAGNOSTIC") {
		t.Fatalf("diagnostic is not navigable: state=%v\n%s", m.State, m.View())
	}
}

func TestBackgroundReviewDiagnosticsHaveIndependentNavigation(t *testing.T) {
	t.Parallel()
	m := New([]inventory.Skill{{Name: "alpha"}}, nil).WithBackgroundReview(&fakeBackgroundReview{})
	m.Mode = ViewSkills
	m.setStatus("Kept alpha")
	diagnostic := audit.Diagnostic{Code: audit.DiagnosticLLMFallback, Message: "review failed: provider detail"}
	updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Diagnostic: &diagnostic}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	if !strings.Contains(m.View(), "provider detail") {
		t.Fatalf("dedicated key did not open review diagnostic:\n%s", m.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	m.setStatus("Action error: later failure")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "later failure") || strings.Contains(view, "provider detail") {
		t.Fatalf("review diagnostic captured later action details:\n%s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.State != StateSkillStory {
		t.Fatalf("normal enter did not recover after dismiss: state=%v", m.State)
	}
}

func TestBackgroundReviewDiagnosticRouteWorksInAdvertisedModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []ViewMode{ViewGuidedReview, ViewCollections} {
		m := New([]inventory.Skill{{Name: "alpha"}}, nil).WithBackgroundReview(&fakeBackgroundReview{})
		m.Mode = mode
		diagnostic := audit.Diagnostic{Code: audit.DiagnosticLLMFallback, Message: "review failed: cross-mode detail"}
		updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Diagnostic: &diagnostic}})
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
		m = updated.(Model)
		if m.State != StateFeedback || !strings.Contains(m.View(), "cross-mode detail") {
			t.Fatalf("mode %v ignored advertised diagnostic route", mode)
		}
	}
}

func TestBackgroundReviewHeaderFitsSupportedViewports(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ width, height int }{{80, 18}, {80, 24}, {120, 40}, {200, 60}} {
		m := New([]inventory.Skill{{Name: "beta"}}, nil).WithBackgroundReview(&fakeBackgroundReview{})
		progress := audit.Progress{Step: "llm-summary", Current: 12, Total: 100, Detail: "beta"}
		updated, _ := m.Update(backgroundReviewMsg{Event: audit.ReviewEvent{Progress: &progress}})
		m = updated.(Model)
		updated, _ = m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		view := updated.(Model).View()
		if !strings.Contains(view, "LLM summaries 12/100") {
			t.Fatalf("%dx%d missing progress:\n%s", size.width, size.height, view)
		}
		if lines := strings.Split(view, "\n"); len(lines) > size.height {
			t.Fatalf("%dx%d rendered %d lines", size.width, size.height, len(lines))
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size.width {
				t.Fatalf("%dx%d overflow width=%d line=%q", size.width, size.height, lipgloss.Width(line), line)
			}
		}
	}
}

func TestDashboardExitCancelsOutstandingBackgroundReview(t *testing.T) {
	t.Parallel()
	review := &fakeBackgroundReview{started: make(chan struct{}), cancelled: make(chan struct{})}
	m := New(nil, nil).WithBackgroundReview(review)
	cmd := m.Init()
	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- cmd() }()
	select {
	case <-review.started:
	case <-time.After(time.Second):
		t.Fatal("review did not start")
	}
	updated, quit := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(Model)
	if quit == nil {
		t.Fatal("q did not return quit command")
	}
	select {
	case <-review.cancelled:
	case <-time.After(time.Second):
		t.Fatal("review context was not cancelled")
	}
}

type fakeBackgroundReview struct {
	mu          sync.Mutex
	snapshots   []inventorysnapshot.Snapshot
	diagnostics [][]audit.Diagnostic
	started     chan struct{}
	cancelled   chan struct{}
}

func (r *fakeBackgroundReview) Run(ctx context.Context, emit func(audit.ReviewEvent)) error {
	if r.started == nil {
		return nil
	}
	close(r.started)
	<-ctx.Done()
	close(r.cancelled)
	return ctx.Err()
}

func (r *fakeBackgroundReview) Persist(snapshot inventorysnapshot.Snapshot, diagnostics []audit.Diagnostic, complete bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = append(r.snapshots, snapshot.Clone())
	r.diagnostics = append(r.diagnostics, append([]audit.Diagnostic(nil), diagnostics...))
	return nil
}
