package picker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/ui"
)

func TestHandleOwnsNavigationAndMarkedSelection(t *testing.T) {
	model := New([]string{"alpha", "beta", "gamma"}, Config{Cursor: 1, MultiSelect: true})

	if outcome := model.Handle(" "); outcome != NoOutcome {
		t.Fatalf("toggle outcome = %v", outcome)
	}
	model.Handle("down")
	model.Handle("down")
	model.Handle(" ")
	model.Handle("up")
	model.Handle(" ")

	selection := model.Selection()
	if selection.Cursor != 1 || selection.Marked[1] || !selection.Marked[2] {
		t.Fatalf("selection = %+v", selection)
	}
	if outcome := model.Handle("enter"); outcome != Submit {
		t.Fatalf("enter outcome = %v", outcome)
	}
	if outcome := model.Handle("esc"); outcome != Cancel {
		t.Fatalf("escape outcome = %v", outcome)
	}
}

func TestExtraChoiceCannotBeMarked(t *testing.T) {
	model := New([]string{"one", "two"}, Config{MultiSelect: true, ExtraChoice: "All 2 installs"})
	model.Handle("down")
	model.Handle("down")
	model.Handle(" ")

	selection := model.Selection()
	if selection.Cursor != 2 || len(selection.Marked) != 0 {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestSelectionDoesNotExposeMutableMarks(t *testing.T) {
	model := New([]string{"one"}, Config{Marked: map[int]bool{0: true}, MultiSelect: true})
	selection := model.Selection()
	delete(selection.Marked, 0)

	if !model.Selection().Marked[0] {
		t.Fatal("selection mutation changed picker state")
	}
}

func TestViewKeepsCursorAndCompleteSelectedLabelVisible(t *testing.T) {
	labels := make([]string, 30)
	for i := range labels {
		labels[i] = fmt.Sprintf("/tmp/fixtures/a-very-long-root-%02d/skills/final-skill-%02d", i, i)
	}
	model := New(labels, Config{Cursor: len(labels) - 1})
	model.Resize(28, 5)

	view := strings.Join(model.View(ui.DefaultTheme()), "\n")
	if !strings.Contains(view, "skill-29") {
		t.Fatalf("selected label tail is hidden:\n%s", view)
	}
	if lipgloss.Height(view) > 5 {
		t.Fatalf("view height = %d:\n%s", lipgloss.Height(view), view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 28 {
			t.Fatalf("line width = %d: %q", lipgloss.Width(line), line)
		}
	}
}

func TestPageKeysExposeEveryLineOfLongSelectedLabel(t *testing.T) {
	segments := make([]string, 12)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment%02d", i)
	}
	model := New([]string{"before", strings.Join(segments, "/") + "/EXACT-TARGET", "after"}, Config{Cursor: 1})
	model.Resize(18, 4)

	seen := map[string]bool{}
	for range 30 {
		view := strings.Join(model.View(ui.DefaultTheme()), "\n")
		for _, segment := range segments {
			seen[segment] = seen[segment] || strings.Contains(view, segment)
		}
		seen["EXACT-TARGET"] = seen["EXACT-TARGET"] || strings.Contains(view, "EXACT-TARGET")
		model.Handle("pgdown")
	}
	for _, want := range append(segments, "EXACT-TARGET") {
		if !seen[want] {
			t.Errorf("selected label segment %q was never visible", want)
		}
	}
	for range 30 {
		model.Handle("pgup")
	}
	if view := strings.Join(model.View(ui.DefaultTheme()), "\n"); !strings.Contains(view, "segment00") {
		t.Fatalf("page-up did not restore selected label start:\n%s", view)
	}
}

func TestEmptyViewAndSelection(t *testing.T) {
	model := New(nil, Config{EmptyLabel: "No choices available"})
	model.Resize(20, 2)

	selection := model.Selection()
	if selection.HasChoice {
		t.Fatalf("selection = %+v", selection)
	}
	if view := strings.Join(model.View(ui.DefaultTheme()), "\n"); !strings.Contains(view, "No choices availa") {
		t.Fatalf("view = %q", view)
	}
}
