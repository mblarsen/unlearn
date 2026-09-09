// Package review builds and advances one-decision-at-a-time maintenance reviews.
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// Action is a durable decision for one item in the active review scope.
type Action string

const (
	ActionKeep       Action = "keep"
	ActionQuarantine Action = "quarantine"
	ActionRevisit    Action = "revisit"
)

// ScopeItem preserves the exact identity and order of an active review.
type ScopeItem struct {
	ID          string `toml:"id"`
	FindingID   string `toml:"finding_id"`
	SkillName   string `toml:"skill_name"`
	InstallPath string `toml:"install_path"`
}

// Decision records how one item was resolved in the active scope.
type Decision struct {
	ItemID string `toml:"item_id"`
	Action Action `toml:"action"`
}

// State is the human-editable TOML state needed to resume a review.
type State struct {
	Scope     []ScopeItem `toml:"scope"`
	Decisions []Decision  `toml:"decisions"`
	Completed bool        `toml:"completed"`
}

// Item contains authoritative evidence and one exact installed skill.
type Item struct {
	ID          string
	Finding     analysis.Finding
	Target      inventory.Skill
	Evidence    []string
	Consequence string
}

// Session hides scope stability, missing-install handling, progress, and resume.
type Session struct {
	state    State
	items    map[string]Item
	current  string
	reviewed int
}

// Start starts a new scope or resumes an incomplete saved scope.
func Start(findings []analysis.Finding, keptSkills []string, saved State) Session {
	available := buildItems(findings, keptSkills)
	if saved.Completed || len(saved.Scope) == 0 {
		saved = State{Scope: scopeFor(available)}
	}

	return resumeWithItems(available, saved)
}

// Resume refreshes install availability without starting a new scope.
func Resume(findings []analysis.Finding, keptSkills []string, saved State) Session {
	return resumeWithItems(buildItems(findings, keptSkills), saved)
}

// Current returns the one item that needs a decision.
func (s Session) Current() (Item, bool) {
	item, ok := s.items[s.current]
	return item, ok
}

// Decide records the current decision and advances to the next item.
func (s Session) Decide(action Action) (Session, error) {
	if action != ActionKeep && action != ActionQuarantine && action != ActionRevisit {
		return s, errors.New("unknown review decision")
	}
	if s.current == "" {
		return s, errors.New("review has no current item")
	}
	s.state.Decisions = append(s.state.Decisions, Decision{ItemID: s.current, Action: action})
	s.state.Completed = false
	items := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	return resumeWithItems(items, s.state), nil
}

func resumeWithItems(available []Item, state State) Session {
	s := Session{state: cloneState(state), items: make(map[string]Item, len(available))}
	for _, item := range available {
		s.items[item.ID] = item
	}
	decided := decisionSet(s.state.Decisions)
	for _, scoped := range s.state.Scope {
		_, exists := s.items[scoped.ID]
		_, resolved := decided[scoped.ID]
		if resolved || !exists {
			s.reviewed++
			continue
		}
		if s.current == "" {
			s.current = scoped.ID
		}
	}
	s.state.Completed = s.reviewed == len(s.state.Scope)
	return s
}

func (s Session) State() State   { return cloneState(s.state) }
func (s Session) Total() int     { return len(s.state.Scope) }
func (s Session) Reviewed() int  { return s.reviewed }
func (s Session) Complete() bool { return s.state.Completed }

func (s Session) CompletionMessage() string {
	return "Review scope complete. This does not mean the global inventory is clean."
}

func buildItems(findings []analysis.Finding, keptSkills []string) []Item {
	kept := make(map[string]bool, len(keptSkills))
	for _, name := range keptSkills {
		kept[strings.ToLower(strings.TrimSpace(name))] = true
	}
	ordered := append([]analysis.Finding(nil), findings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Severity != ordered[j].Severity {
			return ordered[i].Severity < ordered[j].Severity
		}
		return ordered[i].ID < ordered[j].ID
	})
	var items []Item
	for _, finding := range ordered {
		if !eligible(finding.Type) {
			continue
		}
		skills := append([]inventory.Skill(nil), finding.Skills...)
		sort.SliceStable(skills, func(i, j int) bool { return exactPath(skills[i]) < exactPath(skills[j]) })
		for _, skill := range skills {
			if kept[strings.ToLower(strings.TrimSpace(skill.Name))] {
				continue
			}
			items = append(items, Item{
				ID: itemID(finding.ID, skill), Finding: finding, Target: skill,
				Evidence:    append([]string(nil), finding.Reasons...),
				Consequence: consequence(finding.Type, skill),
			})
		}
	}
	return items
}

func eligible(typ analysis.FindingType) bool {
	return typ == analysis.FindingDuplicate || typ == analysis.FindingConflict || typ == analysis.FindingUnseen
}

func consequence(typ analysis.FindingType, skill inventory.Skill) string {
	switch typ {
	case analysis.FindingDuplicate:
		return "Keeping preserves this skill name. Quarantine moves only this exact install and can be restored."
	case analysis.FindingConflict:
		return "The installs have different content. Compare them before you quarantine this exact install."
	case analysis.FindingUnseen:
		return "Not observed does not mean unused. Keep it, quarantine this exact install, or revisit it in a later review."
	default:
		return "Review this exact install before you change it."
	}
}

func itemID(findingID string, skill inventory.Skill) string {
	sum := sha256.Sum256([]byte(findingID + "\n" + exactPath(skill)))
	return hex.EncodeToString(sum[:8])
}

func exactPath(skill inventory.Skill) string {
	for _, value := range []string{skill.EncounteredPath, skill.PrimaryPath, skill.ResolvedPath, skill.ID} {
		if strings.TrimSpace(value) != "" {
			return filepath.Clean(value)
		}
	}
	return skill.Name
}

func scopeFor(items []Item) []ScopeItem {
	scope := make([]ScopeItem, 0, len(items))
	for _, item := range items {
		scope = append(scope, ScopeItem{ID: item.ID, FindingID: item.Finding.ID, SkillName: item.Target.Name, InstallPath: exactPath(item.Target)})
	}
	return scope
}

func decisionSet(decisions []Decision) map[string]Action {
	set := make(map[string]Action, len(decisions))
	for _, decision := range decisions {
		set[decision.ItemID] = decision.Action
	}
	return set
}

func cloneState(state State) State {
	state.Scope = append([]ScopeItem(nil), state.Scope...)
	state.Decisions = append([]Decision(nil), state.Decisions...)
	return state
}
