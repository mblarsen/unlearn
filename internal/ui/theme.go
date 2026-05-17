package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	AppTitle     lipgloss.Style
	Header       lipgloss.Style
	Panel        lipgloss.Style
	PanelTitle   lipgloss.Style
	Section      lipgloss.Style
	Row          lipgloss.Style
	SelectedRow  lipgloss.Style
	Muted        lipgloss.Style
	Accent       lipgloss.Style
	Warning      lipgloss.Style
	Danger       lipgloss.Style
	Success      lipgloss.Style
	Badge        lipgloss.Style
	BadgeWarn    lipgloss.Style
	BadgeDanger  lipgloss.Style
	BadgeSuccess lipgloss.Style
	Keybar       lipgloss.Style
	Key          lipgloss.Style
	Status       lipgloss.Style
}

func DefaultTheme() Theme {
	return Theme{
		AppTitle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5F4FF")),
		Header:       lipgloss.NewStyle().Foreground(lipgloss.Color("#B9C6D3")),
		Panel:        lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#324052")).Padding(0, 1),
		PanelTitle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5F4FF")),
		Section:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D7E1EA")),
		Row:          lipgloss.NewStyle().Foreground(lipgloss.Color("#C9D3DE")),
		SelectedRow:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FBFF")).Background(lipgloss.Color("#243246")).Bold(true),
		Muted:        lipgloss.NewStyle().Foreground(lipgloss.Color("#788696")),
		Accent:       lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC")),
		Warning:      lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")),
		Danger:       lipgloss.NewStyle().Foreground(lipgloss.Color("#FB7185")),
		Success:      lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")),
		Badge:        lipgloss.NewStyle().Foreground(lipgloss.Color("#D9E8F5")).Background(lipgloss.Color("#263445")).Padding(0, 1),
		BadgeWarn:    lipgloss.NewStyle().Foreground(lipgloss.Color("#1F2937")).Background(lipgloss.Color("#FBBF24")).Padding(0, 1).Bold(true),
		BadgeDanger:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF1F2")).Background(lipgloss.Color("#BE123C")).Padding(0, 1).Bold(true),
		BadgeSuccess: lipgloss.NewStyle().Foreground(lipgloss.Color("#052E1A")).Background(lipgloss.Color("#34D399")).Padding(0, 1).Bold(true),
		Keybar:       lipgloss.NewStyle().Foreground(lipgloss.Color("#D7E1EA")).Background(lipgloss.Color("#17202D")).Padding(0, 1),
		Key:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7DD3FC")),
		Status:       lipgloss.NewStyle().Foreground(lipgloss.Color("#A7F3D0")),
	}
}

func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func Wrap(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	words := strings.Fields(strings.ReplaceAll(value, "\n", " "))
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		candidate := line + " " + word
		if lipgloss.Width(candidate) > width {
			lines = append(lines, Truncate(line, width))
			line = word
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, Truncate(line, width))
	}
	return lines
}

func FitLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	out := append([]string(nil), lines[:height]...)
	out[height-1] = Truncate("… more", lipgloss.Width(out[height-1]))
	return out
}

func PadLines(lines []string, height int) []string {
	out := append([]string(nil), lines...)
	for len(out) < height {
		out = append(out, "")
	}
	if len(out) > height {
		return out[:height]
	}
	return out
}
