package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/collections"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/tui/picker"
	"github.com/mblarsen/unlearn/internal/ui"
)

type collectionChoice struct {
	InstallPath string
}

type collectionUIState struct {
	Collections  []collections.Collection
	Preview      collections.CollectionPreview
	Focus        int
	MemberCursor int
	DetailScroll int
	FollowMember bool
	Command      collections.CommandKind
	Picker       picker.Model
	Choices      []collectionChoice
}

func (m *Model) openCollections() {
	service, ok := m.Actions.(CollectionService)
	if !ok {
		m.setStatus("collections are unavailable for this dashboard")
		return
	}
	result := service.ExecuteCollection(collections.Command{Kind: collections.List}, m.Skills)
	if result.Err != nil {
		m.fail(result.Err)
		return
	}
	m.CollectionUI.Collections = result.Collections
	m.Mode = ViewCollections
	m.Cursor = 0
	m.CollectionUI.Focus = 0
	m.CollectionUI.MemberCursor = 0
	m.refreshCollectionPreview()
}

func (m Model) updateCollectionsNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "f":
		m.Mode = ViewFindings
		m.Cursor = 0
		m.CollectionUI.Focus = 0
		return m, nil
	case "?":
		m.State = StateHelp
		return m, nil
	case "enter":
		if m.Status != "" {
			m.FeedbackScroll = 0
			m.State = StateFeedback
		}
		return m, nil
	case "x":
		if m.Status != "" {
			m.dismissStatus()
		} else {
			m.removeSelectedCollectionMember()
		}
		return m, nil
	case "tab", "shift+tab":
		if len(m.CollectionUI.Collections) > 0 {
			m.CollectionUI.Focus = 1 - m.CollectionUI.Focus
			m.CollectionUI.FollowMember = m.CollectionUI.Focus == 1
		}
		return m, nil
	case "j", "down":
		m.moveCollectionCursor(1)
		return m, nil
	case "k", "up":
		m.moveCollectionCursor(-1)
		return m, nil
	case "pgdown", "ctrl+f":
		m.CollectionUI.FollowMember = false
		m.CollectionUI.DetailScroll++
		return m, nil
	case "pgup", "ctrl+b":
		m.CollectionUI.FollowMember = false
		m.CollectionUI.DetailScroll = max(0, m.CollectionUI.DetailScroll-1)
		return m, nil
	case "n":
		m.beginCollectionInput(collections.Create, "Create a collection", "Collection name")
		return m, nil
	case "r":
		if selected, ok := m.selectedCollection(); ok {
			m.beginCollectionInput(collections.Rename, "Rename collection "+selected.Name, "New collection name")
		} else {
			m.setStatus("create a collection before renaming one")
		}
		return m, nil
	case "ctrl+d":
		if selected, ok := m.selectedCollection(); ok {
			m.State = StateCollectionDelete
			m.Message = fmt.Sprintf("Delete collection %q?\nOnly its organization is removed. Installed skills are unchanged.", selected.Name)
		} else {
			m.setStatus("no collection selected")
		}
		return m, nil
	case "a":
		m.beginCollectionAdd()
		return m, nil
	case "s":
		if _, ok := m.selectedCollection(); ok {
			m.beginCollectionInput(collections.Suggest, "Find manual suggestions", "Describe a project or task")
		} else {
			m.setStatus("create a collection before requesting suggestions")
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) moveCollectionCursor(delta int) {
	if m.CollectionUI.Focus == 0 {
		m.Cursor += delta
		m.Cursor = clamp(m.Cursor, 0, max(0, len(m.CollectionUI.Collections)-1))
		m.CollectionUI.MemberCursor = 0
		m.CollectionUI.DetailScroll = 0
		m.refreshCollectionPreview()
		return
	}
	m.CollectionUI.MemberCursor += delta
	m.CollectionUI.MemberCursor = clamp(m.CollectionUI.MemberCursor, 0, max(0, len(m.CollectionUI.Preview.Members)-1))
	m.CollectionUI.DetailScroll = 0
	m.CollectionUI.FollowMember = true
}

func (m *Model) beginCollectionInput(kind collections.CommandKind, message, label string) {
	m.CollectionUI.Command = kind
	m.Input = ""
	m.State = StateCollectionInput
	m.Message = message + "\n" + label
}

func (m Model) updateCollectionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.endCollectionInteraction("collection action cancelled")
	case tea.KeyBackspace, tea.KeyDelete:
		runes := []rune(m.Input)
		if len(runes) > 0 {
			m.Input = string(runes[:len(runes)-1])
		}
	case tea.KeyEnter:
		value := strings.TrimSpace(m.Input)
		if value == "" {
			m.setStatus("input is required")
			return m, nil
		}
		selected, _ := m.selectedCollection()
		command := collections.Command{Kind: m.CollectionUI.Command, Name: selected.Name}
		switch m.CollectionUI.Command {
		case collections.Create:
			command.Name = value
		case collections.Rename:
			command.NewName = value
		case collections.Suggest:
			command.Query = value
		}
		result := m.executeCollection(command)
		if result.Err != nil {
			return m, nil
		}
		if m.CollectionUI.Command == collections.Suggest {
			m.showCollectionSuggestions(result.Suggestions, result.Advisory)
			return m, nil
		}
		m.selectCollectionNamed(value)
		m.endCollectionInteraction(collectionActionStatus(m.CollectionUI.Command, value))
	case tea.KeyRunes:
		m.Input += string(msg.Runes)
	case tea.KeySpace:
		m.Input += " "
	}
	return m, nil
}

func (m Model) updateCollectionDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		selected, ok := m.selectedCollection()
		if !ok {
			m.endCollectionInteraction("no collection selected")
			return m, nil
		}
		name := selected.Name
		result := m.executeCollection(collections.Command{Kind: collections.Delete, Name: name})
		if result.Err == nil {
			m.Cursor = clamp(m.Cursor, 0, max(0, len(m.CollectionUI.Collections)-1))
			m.endCollectionInteraction("deleted collection " + name + "; installed skills were unchanged")
		}
	case "n", "N", "esc":
		m.endCollectionInteraction("collection deletion cancelled")
	}
	return m, nil
}

func (m *Model) beginCollectionAdd() {
	selected, ok := m.selectedCollection()
	if !ok {
		m.setStatus("create a collection before adding a skill")
		return
	}
	memberPaths := map[string]bool{}
	for _, member := range selected.Members {
		memberPaths[member.InstallPath] = true
	}
	var skills []inventory.Skill
	for _, skill := range m.Skills {
		path := collectionSkillPath(skill)
		if path != "" && !memberPaths[path] {
			skills = append(skills, skill)
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		if !strings.EqualFold(skills[i].Name, skills[j].Name) {
			return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
		}
		return collectionSkillPath(skills[i]) < collectionSkillPath(skills[j])
	})
	m.CollectionUI.Choices = nil
	labels := make([]string, 0, len(skills))
	for _, skill := range skills {
		path := collectionSkillPath(skill)
		m.CollectionUI.Choices = append(m.CollectionUI.Choices, collectionChoice{InstallPath: path})
		labels = append(labels, fmt.Sprintf("%s · %s", skill.Name, path))
	}
	m.CollectionUI.Picker = picker.New(labels, picker.Config{EmptyLabel: "No unassigned installed skills"})
	m.State = StateCollectionAdd
	m.Message = "Choose one exact installed skill to add. This does not change agent access."
}

func (m Model) updateCollectionPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.CollectionUI.Picker.Handle(msg.String()) {
	case picker.Cancel:
		m.endCollectionInteraction("collection action cancelled")
	case picker.Submit:
		selection := m.CollectionUI.Picker.Selection()
		if !selection.HasChoice || selection.Cursor >= len(m.CollectionUI.Choices) {
			m.endCollectionInteraction("no installed skill selected")
			return m, nil
		}
		selected, ok := m.selectedCollection()
		if !ok {
			m.endCollectionInteraction("no collection selected")
			return m, nil
		}
		choice := m.CollectionUI.Choices[selection.Cursor]
		result := m.executeCollection(collections.Command{Kind: collections.AddMember, Name: selected.Name, InstallPath: choice.InstallPath})
		if result.Err == nil {
			status := "added exact install to " + selected.Name
			if m.State == StateCollectionSuggestions {
				status = "accepted suggestion and added exact install to " + selected.Name
			}
			m.endCollectionInteraction(status)
		}
	}
	return m, nil
}

func (m *Model) removeSelectedCollectionMember() {
	selected, ok := m.selectedCollection()
	if !ok || len(m.CollectionUI.Preview.Members) == 0 || m.CollectionUI.Focus != 1 {
		m.setStatus("tab to a collection member before removing it")
		return
	}
	idx := clamp(m.CollectionUI.MemberCursor, 0, len(m.CollectionUI.Preview.Members)-1)
	member := m.CollectionUI.Preview.Members[idx].Member
	result := m.executeCollection(collections.Command{Kind: collections.RemoveMember, Name: selected.Name, InstallPath: member.InstallPath})
	if result.Err == nil {
		m.CollectionUI.MemberCursor = clamp(m.CollectionUI.MemberCursor, 0, max(0, len(m.CollectionUI.Preview.Members)-1))
		m.setStatus("removed " + member.SkillName + " from collection; installed skill unchanged")
	}
}

func (m *Model) showCollectionSuggestions(suggestions []collections.Suggestion, advisory string) {
	m.CollectionUI.Choices = nil
	var labels []string
	for _, suggestion := range suggestions {
		strength := "observed match"
		if suggestion.Weak {
			strength = "weak observed match"
		}
		for _, install := range suggestion.Installs {
			m.CollectionUI.Choices = append(m.CollectionUI.Choices, collectionChoice{InstallPath: install.InstallPath})
			labels = append(labels, fmt.Sprintf("%s · %s · %s · %s", suggestion.SkillName, strength, suggestion.Reason, install.InstallPath))
		}
	}
	emptyLabel := strings.TrimSpace(advisory)
	if emptyLabel == "" {
		emptyLabel = "No observed name or description terms matched"
	}
	m.CollectionUI.Picker = picker.New(labels, picker.Config{EmptyLabel: emptyLabel})
	m.State = StateCollectionSuggestions
	m.Message = "Suggestions reuse local task discovery over installed skill names and descriptions. Press enter for manual acceptance."
	m.Input = ""
}

func (m *Model) executeCollection(command collections.Command) collections.Result {
	service, ok := m.Actions.(CollectionService)
	if !ok {
		result := collections.Result{Err: fmt.Errorf("collections are unavailable for this dashboard")}
		m.setStatus("error: " + result.Err.Error())
		return result
	}
	result := service.ExecuteCollection(command, m.Skills)
	if result.Err != nil {
		m.setStatus("error: " + result.Err.Error())
		return result
	}
	m.CollectionUI.Collections = result.Collections
	m.refreshCollectionPreview()
	return result
}

func (m *Model) refreshCollectionPreview() {
	selected, ok := m.selectedCollection()
	if !ok {
		m.CollectionUI.Preview = collections.CollectionPreview{}
		return
	}
	service, ok := m.Actions.(CollectionService)
	if !ok {
		return
	}
	result := service.ExecuteCollection(collections.Command{Kind: collections.Preview, Name: selected.Name}, m.Skills)
	if result.Err == nil {
		m.CollectionUI.Preview = result.Preview
	}
}

func (m Model) selectedCollection() (collections.Collection, bool) {
	if m.Mode != ViewCollections || len(m.CollectionUI.Collections) == 0 || m.Cursor < 0 || m.Cursor >= len(m.CollectionUI.Collections) {
		return collections.Collection{}, false
	}
	return m.CollectionUI.Collections[m.Cursor], true
}

func (m *Model) selectCollectionNamed(name string) {
	for i, item := range m.CollectionUI.Collections {
		if strings.EqualFold(item.Name, strings.TrimSpace(name)) {
			m.Cursor = i
			m.CollectionUI.MemberCursor = 0
			m.CollectionUI.DetailScroll = 0
			return
		}
	}
}

func (m *Model) endCollectionInteraction(status string) {
	m.State = StateNormal
	m.Input = ""
	m.Message = ""
	m.CollectionUI.Command = ""
	m.CollectionUI.Picker = picker.Model{}
	m.CollectionUI.Choices = nil
	m.setStatus(status)
	m.refreshCollectionPreview()
}

func (m Model) renderCollectionRows(theme ui.Theme, width, height int) []string {
	if len(m.CollectionUI.Collections) == 0 {
		return []string{"", theme.Section.Render("No collections"), theme.Muted.Render("Press n to create one.")}
	}
	var rows []string
	for i, item := range m.CollectionUI.Collections {
		prefix := "  "
		style := theme.Row
		if i == m.Cursor {
			prefix = "▸ "
			style = theme.SelectedRow.Width(width)
		}
		label := padBetween(item.Name, fmt.Sprintf("%d members", len(item.Members)), width-2)
		rows = append(rows, style.Render(ui.Truncate(prefix+label, width)))
	}
	return windowLines(rows, height, m.Cursor)
}

func (m Model) renderCollectionDetails(theme ui.Theme, width, height int) []string {
	selected, ok := m.selectedCollection()
	if !ok {
		return boundedCollectionLines([]string{"", theme.Muted.Render("No collection selected"), theme.Muted.Render("Collections organize skills only.")}, width, height)
	}
	lines := []string{"", theme.Accent.Render(ui.Truncate(selected.Name, width))}
	for _, line := range ui.Wrap("Organization only. No install, activation, or agent access changes.", width) {
		lines = append(lines, theme.Muted.Render(line))
	}
	lines = append(lines, "", theme.Section.Render("Members"))
	if len(m.CollectionUI.Preview.Members) == 0 {
		for _, line := range ui.Wrap("No members. Press a to add an exact installed skill.", width) {
			lines = append(lines, theme.Muted.Render(line))
		}
		return boundedCollectionLines(lines, width, height)
	}
	selectedLine := -1
	for i, member := range m.CollectionUI.Preview.Members {
		prefix := "  "
		style := theme.Row
		if m.CollectionUI.Focus == 1 && i == m.CollectionUI.MemberCursor {
			prefix = "▸ "
			style = theme.SelectedRow.Width(width)
			selectedLine = len(lines)
		}
		status := "AVAILABLE"
		if !member.Present {
			status = "STALE"
		}
		lines = append(lines, style.Render(ui.Truncate(fmt.Sprintf("%s%s [%s]", prefix, member.Member.SkillName, status), width)))
		if member.Present {
			visibility := agentEvidence(member.ActiveAgents, member.InactiveAgents)
			for _, line := range ui.Wrap("Agent visibility (inventory evidence): "+visibility, max(1, width-4)) {
				lines = append(lines, theme.Muted.Render("    "+line))
			}
		} else {
			for _, line := range ui.Wrap("Exact path is missing. It was not retargeted by name.", max(1, width-4)) {
				lines = append(lines, theme.Muted.Render("    "+line))
			}
		}
		for _, line := range wrapPreservingText(member.Member.InstallPath, max(1, width-4)) {
			lines = append(lines, theme.Muted.Render("    "+line))
		}
		if len(member.OtherCopies) > 0 {
			divergent := 0
			unknown := 0
			for _, copy := range member.OtherCopies {
				switch copy.Comparison {
				case collections.ContentDivergent:
					divergent++
				case collections.ContentUnknown:
					unknown++
				}
			}
			lines = append(lines, theme.Muted.Render(ui.Truncate(fmt.Sprintf("    Other copies: %d (%d content-divergent, %d unknown)", len(member.OtherCopies), divergent, unknown), width)))
			for _, copy := range member.OtherCopies {
				copyState := "content comparison unknown"
				switch copy.Comparison {
				case collections.ContentEqual:
					copyState = "same observed content"
				case collections.ContentDivergent:
					copyState = "content-divergent"
				}
				for _, line := range ui.Wrap("Alternative copy, not this membership: "+copyState+"; "+agentEvidence(copy.ActiveAgents, copy.InactiveAgents), max(1, width-4)) {
					lines = append(lines, theme.Muted.Render("    "+line))
				}
				for _, line := range wrapPreservingText(copy.InstallPath, max(1, width-6)) {
					lines = append(lines, theme.Muted.Render("      "+line))
				}
			}
		}
	}
	lines = append(lines, "")
	for _, line := range ui.Wrap("Inventory evidence does not confirm that an agent loads or invokes a skill.", width) {
		lines = append(lines, theme.Muted.Render(line))
	}
	if !m.CollectionUI.FollowMember {
		selectedLine = -1
	}
	return boundedCollectionLines(scrollCollectionLines(lines, height, m.CollectionUI.DetailScroll, selectedLine), width, height)
}

func scrollCollectionLines(lines []string, height, scroll, selectedLine int) []string {
	if height <= 0 || len(lines) <= height {
		return ui.FitLines(lines, height)
	}
	page := max(1, height-2)
	start := min(max(0, scroll)*page, max(0, len(lines)-page))
	if selectedLine >= 0 && (selectedLine < start || selectedLine >= start+page) {
		start = min(selectedLine, max(0, len(lines)-page))
	}
	end := min(len(lines), start+page)
	out := make([]string, 0, height)
	if start > 0 {
		out = append(out, "… above")
	}
	out = append(out, lines[start:end]...)
	if end < len(lines) {
		out = append(out, "… more")
	}
	return ui.FitLines(out, height)
}

func boundedCollectionLines(lines []string, width, height int) []string {
	lines = ui.FitLines(lines, height)
	for i := range lines {
		lines[i] = ui.Truncate(lines[i], width)
	}
	return lines
}

func (m Model) renderCollectionInteraction(theme ui.Theme, width, height int) []string {
	lines := []string{theme.BadgeWarn.Render(collectionInteractionTitle(m.State)), ""}
	for _, part := range strings.Split(m.Message, "\n") {
		for _, line := range ui.Wrap(part, width) {
			lines = append(lines, theme.Section.Render(line))
		}
	}
	switch m.State {
	case StateCollectionInput:
		lines = append(lines, "", theme.Muted.Render("Input"), theme.Accent.Render("› ")+ui.Truncate(m.Input, width-2), "", theme.Muted.Render("Options"), theme.Key.Render("enter")+" submit  "+theme.Key.Render("esc")+" cancel")
	case StateCollectionDelete:
		lines = append(lines, "", theme.Muted.Render("Options"), theme.Key.Render("y")+" delete organization  "+theme.Key.Render("n")+" cancel")
	case StateCollectionAdd, StateCollectionSuggestions:
		instruction := "↑/↓ choose exact install · enter add · esc cancel"
		if m.State == StateCollectionSuggestions {
			instruction = "↑/↓ review · enter manual accept · esc cancel"
		}
		lines = appendPickerWindow(lines, theme.Muted.Render(instruction), m.CollectionUI.Picker, theme, width, height)
	}
	return lines
}

func collectionInteractionTitle(state InteractionState) string {
	switch state {
	case StateCollectionInput:
		return "COLLECTION INPUT"
	case StateCollectionDelete:
		return "DELETE COLLECTION ORGANIZATION"
	case StateCollectionAdd:
		return "ADD EXACT INSTALLED SKILL"
	case StateCollectionSuggestions:
		return "MANUAL COLLECTION SUGGESTIONS"
	default:
		return "COLLECTIONS"
	}
}

func collectionActionStatus(kind collections.CommandKind, value string) string {
	switch kind {
	case collections.Create:
		return "created collection " + value
	case collections.Rename:
		return "renamed collection to " + value
	default:
		return "collection updated"
	}
}

func collectionSkillPath(skill inventory.Skill) string {
	path := skill.EncounteredPath
	if path == "" {
		path = skill.PrimaryPath
	}
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func agentEvidence(active, inactive []string) string {
	var parts []string
	if len(active) > 0 {
		parts = append(parts, "active "+strings.Join(active, ", "))
	}
	if len(inactive) > 0 {
		parts = append(parts, "inactive "+strings.Join(inactive, ", "))
	}
	if len(parts) == 0 {
		return "no agent ownership observed"
	}
	return strings.Join(parts, "; ")
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
