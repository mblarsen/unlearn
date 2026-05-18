package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mblarsen/unlearn/internal/inventory"
)

type FindingType string

const (
	FindingDuplicate       FindingType = "duplicate"
	FindingConflict        FindingType = "conflict"
	FindingOverlap         FindingType = "overlap"
	FindingUnseen          FindingType = "unseen"
	FindingHighTokenCost   FindingType = "high-token-cost"
	FindingBroadActivation FindingType = "broad-activation-risk"
	FindingBroken          FindingType = "broken-symlink-reference"
	FindingInactiveRoot    FindingType = "inactive-harness-root"
)

type Finding struct {
	ID       string
	Type     FindingType
	Severity int
	Title    string
	Skills   []inventory.Skill
	Reasons  []string
}

type UsageEvidence map[string]string

type Options struct {
	UsageEvidence  UsageEvidence
	HighTokenLimit int
}

func Analyze(skills []inventory.Skill, opts Options) []Finding {
	if opts.HighTokenLimit == 0 {
		opts.HighTokenLimit = 2000
	}
	var findings []Finding
	findings = append(findings, duplicatesAndConflicts(skills)...)
	findings = append(findings, overlaps(skills)...)
	findings = append(findings, brokenFindings(skills)...)
	findings = append(findings, inactiveRootFindings(skills)...)
	findings = append(findings, groupedSingleSkillFindings(skills, opts)...)
	SortFindings(findings)
	return findings
}

func brokenFindings(skills []inventory.Skill) []Finding {
	groups := map[string][]inventory.Skill{}
	for _, skill := range skills {
		if skill.Broken || len(skill.BrokenRefs) > 0 {
			groups[logicalName(skill)] = append(groups[logicalName(skill)], skill)
		}
	}
	findings := make([]Finding, 0, len(groups))
	for name, group := range groups {
		reasons := []string{}
		brokenCount := 0
		missingRefs := map[string]bool{}
		for _, skill := range group {
			if skill.Broken {
				brokenCount++
			}
			for _, ref := range skill.BrokenRefs {
				missingRefs[ref] = true
			}
		}
		if brokenCount > 0 {
			reasons = append(reasons, fmt.Sprintf("%d install(s) have broken symlinks or unresolved paths", brokenCount))
		}
		refs := sortedKeys(missingRefs)
		if len(refs) > 0 {
			reasons = append(reasons, "missing referenced support files: "+strings.Join(refs, ", "))
		}
		findings = append(findings, Finding{ID: "broken:" + name, Type: FindingBroken, Severity: 1, Title: displayName(group), Skills: group, Reasons: reasons})
	}
	return findings
}

func inactiveRootFindings(skills []inventory.Skill) []Finding {
	groups := map[string][]inventory.Skill{}
	for _, skill := range skills {
		if skill.RootKnown && len(skill.ActiveAgents) == 0 && len(skill.InactiveAgents) > 0 {
			groups[logicalName(skill)] = append(groups[logicalName(skill)], skill)
		}
	}
	findings := make([]Finding, 0, len(groups))
	for name, group := range groups {
		findings = append(findings, Finding{
			ID:       "inactive-root:" + name,
			Type:     FindingInactiveRoot,
			Severity: 2,
			Title:    displayName(group),
			Skills:   group,
			Reasons:  []string{"installed only in skill roots owned by inactive harnesses: " + inactiveAgentsSummary(group)},
		})
	}
	return findings
}

func groupedSingleSkillFindings(skills []inventory.Skill, opts Options) []Finding {
	groups := map[FindingType]map[string][]inventory.Skill{
		FindingHighTokenCost:   {},
		FindingBroadActivation: {},
		FindingUnseen:          {},
	}
	for _, skill := range skills {
		name := logicalName(skill)
		if skill.UpperTokens >= opts.HighTokenLimit {
			groups[FindingHighTokenCost][name] = append(groups[FindingHighTokenCost][name], skill)
		}
		if skill.ActivationRisk == "high" {
			groups[FindingBroadActivation][name] = append(groups[FindingBroadActivation][name], skill)
		}
		if opts.UsageEvidence != nil {
			grade := opts.UsageEvidence[logicalName(skill)]
			if grade == "" || grade == "weak" {
				groups[FindingUnseen][name] = append(groups[FindingUnseen][name], skill)
			}
		}
	}
	var findings []Finding
	for typ, byName := range groups {
		for name, group := range byName {
			switch typ {
			case FindingHighTokenCost:
				findings = append(findings, Finding{ID: "tokens:" + name, Type: typ, Severity: 3, Title: displayName(group), Skills: group, Reasons: []string{fmt.Sprintf("estimated token range %s exceeds %d", tokenRange(group), opts.HighTokenLimit)}})
			case FindingBroadActivation:
				findings = append(findings, Finding{ID: "activation:" + name, Type: typ, Severity: 3, Title: displayName(group), Skills: group, Reasons: []string{"description/body contains broad trigger language"}})
			case FindingUnseen:
				findings = append(findings, Finding{ID: "unseen:" + name, Type: typ, Severity: 4, Title: displayName(group), Skills: group, Reasons: []string{"no strong or medium invocation evidence found in opted-in history"}})
			}
		}
	}
	return findings
}

func duplicatesAndConflicts(skills []inventory.Skill) []Finding {
	byName := map[string][]inventory.Skill{}
	for _, skill := range skills {
		byName[logicalName(skill)] = append(byName[logicalName(skill)], skill)
	}
	var findings []Finding
	for name, group := range byName {
		if len(group) < 2 || !groupHasSharedActiveReader(group) {
			continue
		}
		byHash := map[string][]inventory.Skill{}
		for _, skill := range group {
			byHash[skill.ContentHash] = append(byHash[skill.ContentHash], skill)
		}
		if len(byHash) == 1 {
			findings = append(findings, Finding{ID: "duplicate:" + name, Type: FindingDuplicate, Severity: 1, Title: displayName(group), Skills: group, Reasons: []string{"same skill name and identical effective content visible to at least one active harness"}})
			continue
		}
		findings = append(findings, Finding{ID: "conflict:" + name, Type: FindingConflict, Severity: 1, Title: displayName(group), Skills: group, Reasons: []string{"same skill name but different effective content visible to at least one active harness"}})
	}
	return findings
}

func overlaps(skills []inventory.Skill) []Finding {
	logicalSkills := representativeSkills(skills)
	graph := map[int]map[int][]string{}
	for i := 0; i < len(logicalSkills); i++ {
		for j := i + 1; j < len(logicalSkills); j++ {
			a, b := logicalSkills[i], logicalSkills[j]
			if strings.EqualFold(a.Name, b.Name) {
				continue
			}
			shared := sharedKeywords(a, b)
			if len(shared) >= 2 {
				if graph[i] == nil {
					graph[i] = map[int][]string{}
				}
				if graph[j] == nil {
					graph[j] = map[int][]string{}
				}
				graph[i][j] = shared
				graph[j][i] = shared
			}
		}
	}
	visited := map[int]bool{}
	var findings []Finding
	for start := range graph {
		if visited[start] {
			continue
		}
		component := collectComponent(start, graph, visited)
		if len(component) < 2 {
			continue
		}
		componentSkills := make([]inventory.Skill, 0, len(component))
		terms := map[string]bool{}
		for _, idx := range component {
			componentSkills = append(componentSkills, logicalSkills[idx])
			for _, shared := range graph[idx] {
				for _, term := range shared {
					terms[term] = true
				}
			}
		}
		sort.Slice(componentSkills, func(i, j int) bool { return componentSkills[i].Name < componentSkills[j].Name })
		names := make([]string, 0, len(componentSkills))
		for _, skill := range componentSkills {
			names = append(names, skill.Name)
		}
		sharedTerms := sortedKeys(terms)
		if len(sharedTerms) > 6 {
			sharedTerms = sharedTerms[:6]
		}
		findings = append(findings, Finding{ID: "overlap:" + strings.Join(lowerNames(names), ":"), Type: FindingOverlap, Severity: 2, Title: summarizeNames(names), Skills: componentSkills, Reasons: []string{"shared domain keywords: " + strings.Join(sharedTerms, ", ")}})
	}
	return findings
}

func groupHasSharedActiveReader(group []inventory.Skill) bool {
	for i := 0; i < len(group); i++ {
		for j := i + 1; j < len(group); j++ {
			if group[i].Root == group[j].Root || intersects(group[i].ActiveAgents, group[j].ActiveAgents) {
				return true
			}
			if len(group[i].ActiveAgents) == 0 && len(group[i].InactiveAgents) == 0 && len(group[j].ActiveAgents) == 0 && len(group[j].InactiveAgents) == 0 {
				return true
			}
		}
	}
	return false
}

func intersects(a, b []string) bool {
	seen := map[string]bool{}
	for _, value := range a {
		seen[value] = true
	}
	for _, value := range b {
		if seen[value] {
			return true
		}
	}
	return false
}

func inactiveAgentsSummary(skills []inventory.Skill) string {
	agents := map[string]bool{}
	for _, skill := range skills {
		for _, agent := range skill.InactiveAgents {
			agents[agent] = true
		}
	}
	values := sortedKeys(agents)
	if len(values) == 0 {
		return "unknown"
	}
	return strings.Join(values, ", ")
}

func representativeSkills(skills []inventory.Skill) []inventory.Skill {
	byName := map[string]inventory.Skill{}
	for _, skill := range skills {
		name := logicalName(skill)
		if existing, ok := byName[name]; !ok || skill.UpperTokens > existing.UpperTokens {
			byName[name] = skill
		}
	}
	out := make([]inventory.Skill, 0, len(byName))
	for _, skill := range byName {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func collectComponent(start int, graph map[int]map[int][]string, visited map[int]bool) []int {
	queue := []int{start}
	visited[start] = true
	var component []int
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		component = append(component, idx)
		for next := range graph[idx] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	sort.Ints(component)
	return component
}

func sharedKeywords(a, b inventory.Skill) []string {
	ak := keywords(a.Name + " " + a.Description + " " + a.Body)
	bk := keywords(b.Name + " " + b.Description + " " + b.Body)
	var shared []string
	for word := range ak {
		if bk[word] {
			shared = append(shared, word)
		}
	}
	sort.Strings(shared)
	if len(shared) > 5 {
		return shared[:5]
	}
	return shared
}

func keywords(text string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r < 'a' || r > 'z' })
	out := map[string]bool{}
	for _, field := range fields {
		if len(field) < 4 || genericKeyword(field) {
			continue
		}
		out[field] = true
	}
	return out
}

func genericKeyword(word string) bool {
	stop := map[string]bool{
		"about": true, "across": true, "action": true, "actions": true, "agent": true, "agents": true, "allow": true, "always": true, "area": true, "areas": true, "around": true, "assist": true, "assisted": true, "before": true, "body": true, "broad": true, "budget": true, "budgets": true, "build": true, "check": true, "cleanup": true, "code": true, "coding": true, "common": true, "component": true, "content": true, "create": true, "debug": true, "describe": true, "description": true, "design": true, "develop": true, "development": true, "does": true, "domain": true, "edit": true, "enable": true, "enhance": true, "enough": true, "every": true, "exceed": true, "feature": true, "find": true, "fix": true, "from": true, "generate": true, "generated": true, "generic": true, "help": true, "implement": true, "improve": true, "instance": true, "instances": true, "language": true, "local": true, "long": true, "manage": true, "management": true, "many": true, "material": true, "modify": true, "must": true, "optimize": true, "plan": true, "plus": true, "product": true, "project": true, "read": true, "refactor": true, "request": true, "requests": true, "review": true, "scan": true, "skill": true, "skills": true, "specific": true, "summary": true, "summaries": true, "support": true, "task": true, "tasks": true, "that": true, "things": true, "this": true, "token": true, "tokens": true, "tool": true, "tools": true, "trigger": true, "use": true, "used": true, "user": true, "when": true, "with": true, "words": true, "work": true, "workflow": true, "your": true,
	}
	return stop[word]
}

func SortFindings(findings []Finding) {
	order := map[FindingType]int{
		FindingDuplicate:       1,
		FindingConflict:        2,
		FindingBroken:          3,
		FindingInactiveRoot:    4,
		FindingOverlap:         5,
		FindingHighTokenCost:   6,
		FindingBroadActivation: 7,
		FindingUnseen:          8,
	}
	sort.Slice(findings, func(i, j int) bool {
		if order[findings[i].Type] != order[findings[j].Type] {
			return order[findings[i].Type] < order[findings[j].Type]
		}
		if len(findings[i].Skills) != len(findings[j].Skills) {
			return len(findings[i].Skills) > len(findings[j].Skills)
		}
		return findings[i].Title < findings[j].Title
	})
}

func FindingCounts(findings []Finding) map[FindingType]int {
	counts := map[FindingType]int{}
	for _, finding := range findings {
		counts[finding.Type]++
	}
	return counts
}

func logicalName(skill inventory.Skill) string {
	return strings.ToLower(strings.TrimSpace(skill.Name))
}

func displayName(skills []inventory.Skill) string {
	if len(skills) == 0 {
		return "unknown"
	}
	return skills[0].Name
}

func tokenRange(skills []inventory.Skill) string {
	if len(skills) == 0 {
		return "0-0"
	}
	minLower := skills[0].LowerTokens
	maxUpper := skills[0].UpperTokens
	for _, skill := range skills[1:] {
		if skill.LowerTokens < minLower {
			minLower = skill.LowerTokens
		}
		if skill.UpperTokens > maxUpper {
			maxUpper = skill.UpperTokens
		}
	}
	return fmt.Sprintf("%d-%d", minLower, maxUpper)
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lowerNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = strings.ToLower(name)
	}
	return out
}

func summarizeNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, " / ")
	}
	return fmt.Sprintf("%s / %s / %s +%d", names[0], names[1], names[2], len(names)-3)
}
