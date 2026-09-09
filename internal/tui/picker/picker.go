package picker

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/ui"
)

// Outcome describes an interaction the caller must handle outside the picker.
type Outcome uint8

const (
	NoOutcome Outcome = iota
	Submit
	Cancel
)

// Config defines picker behavior without exposing its navigation or viewport state.
type Config struct {
	Cursor      int
	Marked      map[int]bool
	MultiSelect bool
	ExtraChoice string
	EmptyLabel  string
}

// Selection is the raw picker selection. Domain policy decides how to resolve it.
type Selection struct {
	Cursor    int
	Marked    map[int]bool
	HasChoice bool
}

// Model owns navigation, marked selection, and the bounded row viewport.
type Model struct {
	labels      []string
	cursor      int
	marked      map[int]bool
	multiSelect bool
	extraChoice string
	emptyLabel  string
	width       int
	height      int
	scroll      int
}

func New(labels []string, config Config) Model {
	model := Model{
		labels:      append([]string(nil), labels...),
		cursor:      config.Cursor,
		marked:      cloneMarked(config.Marked),
		multiSelect: config.MultiSelect,
		extraChoice: config.ExtraChoice,
		emptyLabel:  config.EmptyLabel,
	}
	model.clampCursor()
	return model
}

// Handle applies one key and returns only intents owned by the caller.
func (m *Model) Handle(key string) Outcome {
	switch key {
	case "j", "down":
		if m.cursor < m.choiceCount()-1 {
			m.cursor++
			m.scroll = 0
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.scroll = 0
		}
	case "pgdown":
		m.scroll++
	case "pgup":
		m.scroll = max(0, m.scroll-1)
	case " ":
		if m.multiSelect && m.cursor >= 0 && m.cursor < len(m.labels) {
			m.marked[m.cursor] = !m.marked[m.cursor]
		}
	case "enter":
		return Submit
	case "esc", "q":
		return Cancel
	}
	return NoOutcome
}

// Resize changes the bounded row viewport used by View.
func (m *Model) Resize(width, height int) {
	m.width = width
	m.height = height
}

// Selection returns a defensive copy of the raw cursor and marks.
func (m Model) Selection() Selection {
	return Selection{
		Cursor:    m.cursor,
		Marked:    cloneMarked(m.marked),
		HasChoice: m.choiceCount() > 0,
	}
}

// View renders only the rows visible in the current viewport.
func (m Model) View(theme ui.Theme) []string {
	if m.choiceCount() == 0 {
		if m.emptyLabel == "" {
			return nil
		}
		return windowSelectedLines([]string{theme.Muted.Render(ui.Truncate("  "+m.emptyLabel, m.width))}, m.height, 0, 1, 0)
	}

	rows := make([]string, 0, m.choiceCount())
	selectedStart := 0
	selectedEnd := 0
	for i, label := range m.labels {
		if m.multiSelect {
			mark := "[ ] "
			if m.marked[i] {
				mark = "[x] "
			}
			label = mark + label
		}
		var first int
		rows, first = appendChoiceRows(rows, theme, label, i == m.cursor, m.width)
		if i == m.cursor {
			selectedStart = first
			selectedEnd = len(rows) - 1
		}
	}
	if m.extraChoice != "" {
		var first int
		rows, first = appendChoiceRows(rows, theme, m.extraChoice, m.cursor == len(m.labels), m.width)
		if m.cursor == len(m.labels) {
			selectedStart = first
			selectedEnd = len(rows) - 1
		}
	}
	return windowSelectedLines(rows, m.height, selectedStart, selectedEnd-selectedStart+1, m.scroll)
}

func (m *Model) clampCursor() {
	count := m.choiceCount()
	if count == 0 || m.cursor < 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= count {
		m.cursor = count - 1
	}
}

func (m Model) choiceCount() int {
	count := len(m.labels)
	if m.extraChoice != "" {
		count++
	}
	return count
}

func cloneMarked(marked map[int]bool) map[int]bool {
	clone := make(map[int]bool, len(marked))
	for index, selected := range marked {
		if selected {
			clone[index] = true
		}
	}
	return clone
}

func appendChoiceRows(rows []string, theme ui.Theme, label string, selected bool, width int) ([]string, int) {
	first := len(rows)
	if !selected {
		return append(rows, theme.Row.Render(ui.Truncate("  "+label, width))), first
	}
	style := theme.SelectedRow.Width(width)
	for _, line := range wrapPreservingText("▸ "+label, width) {
		rows = append(rows, style.Render(line))
	}
	return rows, first
}

func wrapPreservingText(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	remaining := []rune(strings.ReplaceAll(value, "\n", " "))
	for len(remaining) > 0 {
		end := 0
		for end < len(remaining) && lipgloss.Width(string(remaining[:end+1])) <= width {
			end++
		}
		if end == 0 {
			end = 1
		}
		breakAt := end
		if end < len(remaining) {
			for i := end - 1; i > 0; i-- {
				if remaining[i] == '/' || remaining[i] == ' ' {
					breakAt = i + 1
					break
				}
			}
		}
		lines = append(lines, strings.TrimRight(string(remaining[:breakAt]), " "))
		remaining = remaining[breakAt:]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func windowSelectedLines(lines []string, height, selectedLine, selectedHeight, scroll int) []string {
	if height <= 0 || len(lines) <= height {
		return ui.FitLines(lines, height)
	}
	selectedEnd := min(len(lines), selectedLine+max(1, selectedHeight))
	showAbove := selectedLine > 0 || scroll > 0
	showBelow := selectedEnd < len(lines)
	contentHeight := height
	if showAbove {
		contentHeight--
	}
	if showBelow {
		contentHeight--
	}
	contentHeight = max(1, contentHeight)

	if selectedHeight > contentHeight {
		maxScroll := max(0, selectedHeight-contentHeight)
		scroll = min(max(0, scroll), maxScroll)
		showAbove = selectedLine > 0 || scroll > 0
		contentHeight = height
		if showAbove {
			contentHeight--
		}
		if showBelow {
			contentHeight--
		}
		contentHeight = max(1, contentHeight)
		maxScroll = max(0, selectedHeight-contentHeight)
		scroll = min(scroll, maxScroll)
		start := selectedLine + scroll
		end := min(selectedEnd, start+contentHeight)
		return addWindowIndicators(lines[start:end], showAbove, end < len(lines), height)
	}

	contextHeight := max(0, contentHeight-selectedHeight)
	before := min(selectedLine, contextHeight/2)
	after := min(len(lines)-selectedEnd, contextHeight-before)
	before = min(selectedLine, contextHeight-after)
	start := selectedLine - before
	end := selectedEnd + after
	return addWindowIndicators(lines[start:end], start > 0, end < len(lines), height)
}

func addWindowIndicators(content []string, above, below bool, height int) []string {
	out := make([]string, 0, height)
	if above {
		out = append(out, "… above")
	}
	out = append(out, content...)
	if below && len(out) < height {
		out = append(out, "… more")
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
