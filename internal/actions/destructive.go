package actions

import (
	"errors"
	"sort"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// DestructiveKind identifies dashboard actions whose target selection is safety-critical.
type DestructiveKind int

const (
	DestructiveUnknown DestructiveKind = iota
	DestructiveQuarantine
	DestructiveDelete
	DestructiveRename
)

// SelectionInput captures the exact-install selection state from the UI adapter.
// It keeps the domain rule for marked installs vs highlighted install vs explicit all-installs
// outside the TUI state machine.
type SelectionInput struct {
	Kind     DestructiveKind
	Choices  []inventory.Skill
	Cursor   int
	Marked   map[int]bool
	AllowAll bool
}

var ErrNoInstallSelected = errors.New("no install selected")

// AllowsAllInstalls reports whether a destructive action may expose an explicit all-installs target.
func AllowsAllInstalls(kind DestructiveKind, choices []inventory.Skill) bool {
	return (kind == DestructiveQuarantine || kind == DestructiveDelete) && len(choices) > 1
}

// ResolveSelection returns the installs selected by an exact-install action modal.
// Marked installs win over the cursor. If the cursor is on the explicit all-installs row,
// every choice is selected. Otherwise the highlighted exact install is selected.
func ResolveSelection(input SelectionInput) ([]inventory.Skill, error) {
	if len(input.Choices) == 0 {
		return nil, ErrNoInstallSelected
	}
	var selected []inventory.Skill
	for i, skill := range input.Choices {
		if input.Marked[i] {
			selected = append(selected, skill)
		}
	}
	if len(selected) > 0 {
		return selected, nil
	}
	if input.AllowAll && AllowsAllInstalls(input.Kind, input.Choices) && input.Cursor == len(input.Choices) {
		return append([]inventory.Skill(nil), input.Choices...), nil
	}
	cursor := input.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(input.Choices) {
		cursor = len(input.Choices) - 1
	}
	return []inventory.Skill{input.Choices[cursor]}, nil
}

// FirstMissingWrite returns the first exact install whose trusted root lacks write permission.
func FirstMissingWrite(cfg config.Config, skills []inventory.Skill) (inventory.Skill, bool) {
	for _, skill := range skills {
		if !cfg.CanWrite(skill.Root) {
			return skill, true
		}
	}
	return inventory.Skill{}, false
}

// BatchRootChoice is a safe batch duplicate cleanup target: duplicate installs from one root,
// with at most one install selected per duplicate finding.
type BatchRootChoice struct {
	Root   string
	Skills []inventory.Skill
}

// DuplicateRootChoices derives batch duplicate cleanup choices from current findings.
// Only duplicate findings are eligible, and each root contributes at most one install per finding.
func DuplicateRootChoices(findings []analysis.Finding) []BatchRootChoice {
	byRoot := map[string]map[string]inventory.Skill{}
	for _, finding := range findings {
		if finding.Type != analysis.FindingDuplicate || len(finding.Skills) < 2 {
			continue
		}
		for _, skill := range finding.Skills {
			if byRoot[skill.Root] == nil {
				byRoot[skill.Root] = map[string]inventory.Skill{}
			}
			byRoot[skill.Root][finding.ID] = skill
		}
	}
	choices := make([]BatchRootChoice, 0, len(byRoot))
	for root, byFinding := range byRoot {
		if len(byFinding) == 0 {
			continue
		}
		skills := make([]inventory.Skill, 0, len(byFinding))
		for _, skill := range byFinding {
			skills = append(skills, skill)
		}
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		choices = append(choices, BatchRootChoice{Root: root, Skills: skills})
	}
	sort.Slice(choices, func(i, j int) bool {
		if len(choices[i].Skills) != len(choices[j].Skills) {
			return len(choices[i].Skills) > len(choices[j].Skills)
		}
		return choices[i].Root < choices[j].Root
	})
	return choices
}
