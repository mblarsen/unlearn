package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/story"
	"github.com/mblarsen/unlearn/internal/ui"
)

func (m Model) updateSkillStory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll, pageSize := m.skillStoryScrollMetrics()
	switch msg.String() {
	case "j", "down":
		m.StoryScroll = min(maxScroll, m.StoryScroll+1)
	case "k", "up":
		m.StoryScroll = max(0, m.StoryScroll-1)
	case "pgdown", "ctrl+d":
		m.StoryScroll = min(maxScroll, m.StoryScroll+pageSize)
	case "pgup", "ctrl+u":
		m.StoryScroll = max(0, m.StoryScroll-pageSize)
	case "home", "g":
		m.StoryScroll = 0
	case "end", "G":
		m.StoryScroll = maxScroll
	case "esc", "q", "enter":
		m.StoryScroll = 0
		m.State = StateNormal
	}
	return m, nil
}

func (m Model) renderSkillStory(theme ui.Theme, width, height int) []string {
	group, ok := m.selectedSkillGroup()
	if !ok {
		return []string{theme.Badge.Render("SKILL STORY"), "", theme.Muted.Render("No skill is selected."), "", theme.Muted.Render("esc back")}
	}
	account := story.Build(group.Skills, story.Coverage(m.EvidenceCoverage))
	content := renderSkillStoryContent(theme, account, width)
	available := max(1, height-3)
	windowHeight := available
	if len(content) > available {
		windowHeight = max(1, available-1)
	}
	maxScroll := max(0, len(content)-windowHeight)
	start := min(maxScroll, max(0, m.StoryScroll))

	lines := []string{theme.Badge.Render("SKILL STORY") + " " + theme.Accent.Render(ui.Truncate(account.Name, max(1, width-14))), ""}
	budget := available
	if start > 0 {
		lines = append(lines, theme.Muted.Render("… above"))
		budget--
	}
	end := min(len(content), start+max(1, budget))
	if end < len(content) && budget > 1 {
		end--
	}
	lines = append(lines, content[start:end]...)
	if end < len(content) {
		lines = append(lines, theme.Muted.Render("… more"))
	}
	lines = append(lines, theme.Muted.Render("↑↓ scroll · PgUp/PgDn page · esc back"))
	return ui.FitLines(lines, height)
}

func renderSkillStoryContent(theme ui.Theme, account story.Story, width int) []string {
	var lines []string
	lines = append(lines, theme.Section.Render("Known history"))
	lines = appendStoryFact(lines, theme, "Origin", account.Origin, width)
	lines = appendStoryFact(lines, theme, "Install date", account.InstallDate, width)
	lines = appendStoryFact(lines, theme, "Upstream baseline", account.UpstreamBaseline, width)
	lines = appendStoryFact(lines, theme, "Modification status", account.ModificationStatus, width)

	lines = append(lines, "", theme.Section.Render("Installed locations"))
	for i, install := range account.Installs {
		lines = append(lines, theme.Accent.Render(fmt.Sprintf("Copy %d", i+1)))
		lines = appendStoryFact(lines, theme, "Path", install.Path, width)
		if install.ResolvedPath != "" && (install.Symlink || install.ResolvedPath != install.Path) {
			lines = appendStoryFact(lines, theme, "Resolved path", install.ResolvedPath, width)
		}
		if install.Symlink {
			lines = appendStoryFact(lines, theme, "Filesystem", "symlink", width)
		}
		lines = appendStoryFact(lines, theme, "Kind", unknownIfEmpty(install.Kind), width)
		lines = appendStoryFact(lines, theme, "Source evidence", install.SourceEvidence, width)
		lines = appendStoryFact(lines, theme, "Agent access", install.AgentAccess, width)
		lines = appendStoryFact(lines, theme, "Content hash", unknownIfEmpty(install.ContentHash), width)
	}

	lines = append(lines, "", theme.Section.Render("Installed-copy comparison"))
	lines = appendWrappedStoryText(lines, theme.Muted, account.CopyComparisonNote, width)
	for i, comparison := range account.Comparisons {
		lines = append(lines, theme.Accent.Render(fmt.Sprintf("Copy 1 compared with copy %d", i+2)))
		lines = appendStoryFact(lines, theme, "First", comparison.LeftPath, width)
		lines = appendStoryFact(lines, theme, "Second", comparison.RightPath, width)
		for _, difference := range comparison.Differences {
			lines = appendWrappedStoryText(lines, theme.Row, "• "+difference, width)
		}
	}

	lines = append(lines, "", theme.Section.Render("Derived usage"))
	lines = appendStoryFact(lines, theme, "Coverage", account.Usage.Coverage, width)
	lines = appendStoryFact(lines, theme, "Grade", account.Usage.Status, width)
	lines = appendStoryFact(lines, theme, "Sources", fmt.Sprintf("%d derived source(s)", account.Usage.SourceCount), width)
	lastSeen := "unknown; no observed timestamp"
	if !account.Usage.LastSeen.IsZero() {
		lastSeen = account.Usage.LastSeen.UTC().Format(time.RFC3339)
	}
	lines = appendStoryFact(lines, theme, "Last seen (derived match)", lastSeen, width)
	lines = appendWrappedStoryText(lines, theme.Muted, account.Usage.Attribution, width)
	lines = appendWrappedStoryText(lines, theme.Muted, account.Usage.Limit, width)
	return lines
}

func appendStoryFact(lines []string, theme ui.Theme, label, value string, width int) []string {
	prefix := label + ": "
	wrapped := wrapPreservingText(prefix+value, width)
	for i, line := range wrapped {
		if i == 0 {
			lines = append(lines, theme.Row.Render(line))
		} else {
			lines = append(lines, theme.Muted.Render(line))
		}
	}
	return lines
}

func appendWrappedStoryText(lines []string, style interface{ Render(...string) string }, value string, width int) []string {
	for _, line := range ui.Wrap(value, width) {
		lines = append(lines, style.Render(line))
	}
	return lines
}

func (m Model) skillStoryScrollMetrics() (int, int) {
	group, ok := m.selectedSkillGroup()
	if !ok {
		return 0, 1
	}
	width, height := m.dimensions()
	modalWidth := width - 16
	if modalWidth > 110 {
		modalWidth = 110
	}
	if modalWidth < 52 {
		modalWidth = width - 4
	}
	contentWidth := max(1, modalWidth-6)
	contentHeight := max(1, height-7)
	lines := renderSkillStoryContent(ui.DefaultTheme(), story.Build(group.Skills, story.Coverage(m.EvidenceCoverage)), contentWidth)
	available := max(1, contentHeight-3)
	if len(lines) <= available {
		return 0, available
	}
	lastWindowHeight := max(1, available-1)
	pageSize := max(1, available-2)
	return max(0, len(lines)-lastWindowHeight), pageSize
}

func unknownIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
