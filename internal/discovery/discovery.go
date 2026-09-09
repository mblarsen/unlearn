// Package discovery ranks installed skills against a local task description.
package discovery

import (
	"sort"
	"strings"
	"unicode"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// Result is a deterministic search result. Scores are intentionally not exposed.
type Result struct {
	Query     string
	WeakQuery bool
	Message   string
	Matches   []Match
}

// Match groups all exact installs of one logical skill.
type Match struct {
	Name        string
	Description string
	Reasons     []string
	Installs    []Install
	Invocation  string
	OverlapWith []string
	Weak        bool
	score       int
}

// Install describes one observed inventory entry and its known agent access.
type Install struct {
	Path           string
	Root           string
	ActiveAgents   []string
	InactiveAgents []string
	AccessKnown    bool
	Missing        bool
}

// Search matches a task against observed names and descriptions only.
func Search(query string, skills []inventory.Skill, findings []analysis.Finding) Result {
	result := Result{Query: strings.TrimSpace(query)}
	queryTerms := terms(result.Query)
	if len(queryTerms) == 0 {
		result.WeakQuery = true
		result.Message = "Use more specific terms that describe the task."
		return result
	}

	groups := groupSkills(skills)
	for _, group := range groups {
		match, ok := matchGroup(queryTerms, group)
		if ok {
			result.Matches = append(result.Matches, match)
		}
	}
	sort.SliceStable(result.Matches, func(i, j int) bool {
		if result.Matches[i].score != result.Matches[j].score {
			return result.Matches[i].score > result.Matches[j].score
		}
		return strings.ToLower(result.Matches[i].Name) < strings.ToLower(result.Matches[j].Name)
	})
	if len(result.Matches) == 0 {
		result.Message = "No observed matching installed skill for this task."
		return result
	}
	attachOverlaps(result.Matches, findings)
	return result
}

func groupSkills(skills []inventory.Skill) [][]inventory.Skill {
	byName := map[string][]inventory.Skill{}
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			name = strings.TrimSpace(skill.EncounteredPath)
		}
		byName[strings.ToLower(name)] = append(byName[strings.ToLower(name)], skill)
	}
	keys := make([]string, 0, len(byName))
	for key := range byName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := make([][]inventory.Skill, 0, len(keys))
	for _, key := range keys {
		group := byName[key]
		sort.SliceStable(group, func(i, j int) bool { return installPath(group[i]) < installPath(group[j]) })
		groups = append(groups, group)
	}
	return groups
}

func matchGroup(queryTerms map[string]string, skills []inventory.Skill) (Match, bool) {
	representative := skills[0]
	for _, skill := range skills[1:] {
		if representative.Description == "" && skill.Description != "" {
			representative = skill
		}
	}
	nameMatches := matching(queryTerms, terms(representative.Name))
	descriptionMatches := map[string]string{}
	for _, skill := range skills {
		for canonical, display := range matching(queryTerms, terms(skill.Description)) {
			descriptionMatches[canonical] = display
		}
	}
	if len(nameMatches)+len(descriptionMatches) == 0 {
		return Match{}, false
	}
	matchedTerms := map[string]bool{}
	for term := range nameMatches {
		matchedTerms[term] = true
	}
	for term := range descriptionMatches {
		matchedTerms[term] = true
	}
	match := Match{Name: representative.Name, Description: representative.Description, Weak: len(matchedTerms) < 2}
	if len(nameMatches) > 0 {
		match.Reasons = append(match.Reasons, "name matches: "+joinDisplays(nameMatches))
	}
	if len(descriptionMatches) > 0 {
		match.Reasons = append(match.Reasons, "description matches: "+joinDisplays(descriptionMatches))
	}
	match.score = len(nameMatches)*4 + len(descriptionMatches)*2
	if len(nameMatches) == len(queryTerms) {
		match.score += 3
	}
	match.Invocation = invocationSummary(skills)
	for _, skill := range skills {
		match.Installs = append(match.Installs, Install{
			Path:           installPath(skill),
			Root:           skill.Root,
			ActiveAgents:   sortedCopy(skill.ActiveAgents),
			InactiveAgents: sortedCopy(skill.InactiveAgents),
			AccessKnown:    skill.RootKnown,
			Missing:        skill.Broken,
		})
	}
	return match, true
}

func attachOverlaps(matches []Match, findings []analysis.Finding) {
	matched := map[string]bool{}
	for _, match := range matches {
		matched[strings.ToLower(match.Name)] = true
	}
	for i := range matches {
		name := strings.ToLower(matches[i].Name)
		seen := map[string]bool{}
		for _, finding := range findings {
			if finding.Type != analysis.FindingOverlap || !findingHasName(finding, name) {
				continue
			}
			for _, skill := range finding.Skills {
				other := strings.ToLower(skill.Name)
				if other != name && matched[other] {
					seen[skill.Name] = true
				}
			}
		}
		for other := range seen {
			matches[i].OverlapWith = append(matches[i].OverlapWith, other)
		}
		sort.Slice(matches[i].OverlapWith, func(a, b int) bool {
			return strings.ToLower(matches[i].OverlapWith[a]) < strings.ToLower(matches[i].OverlapWith[b])
		})
	}
}

func findingHasName(finding analysis.Finding, name string) bool {
	for _, skill := range finding.Skills {
		if strings.ToLower(skill.Name) == name {
			return true
		}
	}
	return false
}

func invocationSummary(skills []inventory.Skill) string {
	best := 0
	for _, skill := range skills {
		rank := map[string]int{"weak": 1, "medium": 2, "strong": 3}[strings.ToLower(skill.HistoryEvidence)]
		if rank > best {
			best = rank
		}
	}
	switch best {
	case 3:
		return "observed: strong derived evidence"
	case 2:
		return "observed: medium derived evidence"
	case 1:
		return "mentioned only: weak derived evidence"
	default:
		return "not observed"
	}
}

func matching(query, field map[string]string) map[string]string {
	matches := map[string]string{}
	for canonical, display := range query {
		if field[canonical] != "" {
			matches[canonical] = display
		}
	}
	return matches
}

func joinDisplays(values map[string]string) string {
	out := make([]string, 0, len(values))
	for _, display := range values {
		out = append(out, display)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func terms(text string) map[string]string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	out := map[string]string{}
	for _, field := range fields {
		canonical := canonicalTerm(field)
		if len([]rune(canonical)) < 3 || stopTerms[canonical] {
			continue
		}
		out[canonical] = field
	}
	return out
}

func canonicalTerm(term string) string {
	runes := []rune(term)
	if len(runes) > 5 && strings.HasSuffix(term, "ies") {
		return string(runes[:len(runes)-3]) + "y"
	}
	if len(runes) > 4 && strings.HasSuffix(term, "s") && !strings.HasSuffix(term, "ss") {
		return string(runes[:len(runes)-1])
	}
	return term
}

func installPath(skill inventory.Skill) string {
	for _, path := range []string{skill.EncounteredPath, skill.PrimaryPath, skill.ResolvedPath} {
		if strings.TrimSpace(path) != "" {
			return path
		}
	}
	return skill.Name
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

var stopTerms = map[string]bool{
	"and": true, "for": true, "from": true, "help": true, "into": true, "me": true,
	"need": true, "skill": true, "task": true, "that": true, "the": true, "this": true,
	"use": true, "using": true, "want": true, "with": true,
}
