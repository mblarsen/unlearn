package analysis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
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

type ProgressEvent struct {
	Step    string
	Current int
	Total   int
	Detail  string
	Done    bool
}

type ProgressFunc func(ProgressEvent)

type Options struct {
	UsageEvidence  UsageEvidence
	HighTokenLimit int
	LLMAnalyzer    llm.Analyzer
	Progress       ProgressFunc
}

const minOverlapSharedKeywords = 3

func Analyze(skills []inventory.Skill, opts Options) []Finding {
	findings, _ := AnalyzeWithLLM(context.Background(), skills, opts)
	return findings
}

func AnalyzeWithLLM(ctx context.Context, skills []inventory.Skill, opts Options) ([]Finding, error) {
	if opts.HighTokenLimit == 0 {
		opts.HighTokenLimit = 2000
	}
	var findings []Finding
	findings = append(findings, duplicatesAndConflicts(skills)...)
	findings = append(findings, overlaps(skills)...)
	if opts.LLMAnalyzer != nil {
		llmFindings, err := llmOverlaps(ctx, skills, opts.LLMAnalyzer, opts.Progress)
		if err != nil {
			SortFindings(findings)
			return findings, err
		}
		findings = mergeLLMOverlapFindings(findings, llmFindings)
	}
	findings = append(findings, brokenFindings(skills)...)
	findings = append(findings, inactiveRootFindings(skills)...)
	findings = append(findings, groupedSingleSkillFindings(skills, opts)...)
	SortFindings(findings)
	return findings, nil
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
				findings = append(findings, Finding{ID: "activation:" + name, Type: typ, Severity: 3, Title: displayName(group), Skills: group, Reasons: activationReasons(group)})
			case FindingUnseen:
				findings = append(findings, Finding{ID: "unseen:" + name, Type: typ, Severity: 4, Title: displayName(group), Skills: group, Reasons: []string{"no strong or medium invocation evidence found in opted-in history"}})
			}
		}
	}
	return findings
}

func activationReasons(skills []inventory.Skill) []string {
	signals := map[string]bool{}
	for _, skill := range skills {
		matches := append([]string(nil), skill.ActivationRiskSignals...)
		if len(matches) == 0 {
			matches = inventory.AssessActivationRisk(skill.Description, skill.Body).Signals
		}
		for _, signal := range matches {
			signals[signal] = true
		}
	}
	if len(signals) == 0 {
		return []string{"activation risk marked high"}
	}
	return []string{"matched broad activation signals: " + strings.Join(sortedKeys(signals), ", ")}
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
	keywordSets := make([]map[string]bool, len(logicalSkills))
	for i, skill := range logicalSkills {
		keywordSets[i] = overlapKeywords(skill)
	}
	dropCorpusGenericKeywords(keywordSets)

	graph := map[int]map[int][]string{}
	for i := 0; i < len(logicalSkills); i++ {
		for j := i + 1; j < len(logicalSkills); j++ {
			a, b := logicalSkills[i], logicalSkills[j]
			if strings.EqualFold(a.Name, b.Name) {
				continue
			}
			shared := sharedKeywordSets(keywordSets[i], keywordSets[j])
			if len(shared) >= minOverlapSharedKeywords {
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
		if !denseOverlapComponent(component, graph) {
			for pos, idx := range component {
				for _, next := range component[pos+1:] {
					shared, ok := graph[idx][next]
					if !ok {
						continue
					}
					findings = append(findings, overlapFinding([]inventory.Skill{logicalSkills[idx], logicalSkills[next]}, shared))
				}
			}
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
		findings = append(findings, overlapFinding(componentSkills, sortedKeys(terms)))
	}
	return findings
}

func llmOverlaps(ctx context.Context, skills []inventory.Skill, analyzer llm.Analyzer, progress ProgressFunc) ([]Finding, error) {
	logicalSkills := representativeSkills(skills)
	summaries := make([]llm.GeneratedSummary, 0, len(logicalSkills))
	byName := map[string]inventory.Skill{}
	for index, skill := range logicalSkills {
		reportProgress(progress, ProgressEvent{Step: "llm-summary", Current: index + 1, Total: len(logicalSkills), Detail: skill.Name})
		summary, err := analyzer.Summarize(ctx, skill.Name, deterministicSummary(skill), skill.ContentHash)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
		byName[logicalName(skill)] = skill
	}
	reportProgress(progress, ProgressEvent{Step: "llm-summary", Current: len(logicalSkills), Total: len(logicalSkills), Detail: fmt.Sprintf("%d skill summaries ready", len(summaries)), Done: true})
	reportProgress(progress, ProgressEvent{Step: "llm-overlap", Detail: "asking Gemini to group semantic overlaps"})
	overlaps, err := analyzer.FindOverlaps(ctx, summaries)
	if err != nil {
		return nil, err
	}
	reportProgress(progress, ProgressEvent{Step: "llm-overlap", Current: len(overlaps), Detail: fmt.Sprintf("%d LLM overlap group(s)", len(overlaps)), Done: true})
	findings := make([]Finding, 0, len(overlaps))
	for _, overlap := range overlaps {
		group := make([]inventory.Skill, 0, len(overlap.SkillNames))
		for _, name := range overlap.SkillNames {
			if skill, ok := byName[strings.ToLower(strings.TrimSpace(name))]; ok {
				group = append(group, skill)
			}
		}
		if len(group) < 2 {
			continue
		}
		findings = append(findings, llmOverlapFinding(group, overlap))
	}
	return findings, nil
}

func mergeLLMOverlapFindings(findings, llmFindings []Finding) []Finding {
	emittedLLM := map[string]bool{}
	for _, llmFinding := range llmFindings {
		key := findingSkillSetKey(llmFinding)
		if emittedLLM[key] {
			continue
		}
		merged := false
		for i := range findings {
			if findings[i].Type != FindingOverlap {
				continue
			}
			if findingContainsAllSkills(findings[i], llmFinding) {
				findings[i].Reasons = appendUniqueStrings(findings[i].Reasons, llmFinding.Reasons...)
				merged = true
				break
			}
		}
		if !merged {
			findings = append(findings, llmFinding)
		}
		emittedLLM[key] = true
	}
	return findings
}

func findingContainsAllSkills(container, candidate Finding) bool {
	containerNames := map[string]bool{}
	for _, skill := range container.Skills {
		containerNames[logicalName(skill)] = true
	}
	for _, skill := range candidate.Skills {
		if !containerNames[logicalName(skill)] {
			return false
		}
	}
	return true
}

func findingSkillSetKey(finding Finding) string {
	names := make([]string, 0, len(finding.Skills))
	for _, skill := range finding.Skills {
		names = append(names, logicalName(skill))
	}
	sort.Strings(names)
	return string(finding.Type) + ":" + strings.Join(names, ":")
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return values
}

func reportProgress(progress ProgressFunc, event ProgressEvent) {
	if progress != nil {
		progress(event)
	}
}

func deterministicSummary(skill inventory.Skill) string {
	if strings.TrimSpace(skill.Description) != "" {
		return skill.Description
	}
	return firstWords(skill.Body, 80)
}

func llmOverlapFinding(skills []inventory.Skill, overlap llm.SemanticOverlap) Finding {
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	reason := "LLM-assisted semantic overlap: " + overlap.Reason
	if overlap.Provider != "" || overlap.Model != "" {
		reason += fmt.Sprintf(" (%s/%s)", emptyLabel(overlap.Provider), emptyLabel(overlap.Model))
	}
	return Finding{ID: "llm-overlap:" + strings.Join(lowerNames(names), ":"), Type: FindingOverlap, Severity: 2, Title: summarizeNames(names), Skills: skills, Reasons: []string{reason}}
}

func emptyLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func denseOverlapComponent(component []int, graph map[int]map[int][]string) bool {
	if len(component) <= 2 {
		return true
	}
	edges := 0
	possible := len(component) * (len(component) - 1) / 2
	for pos, idx := range component {
		for _, next := range component[pos+1:] {
			if _, ok := graph[idx][next]; ok {
				edges++
			}
		}
	}
	return edges*4 >= possible*3
}

func overlapFinding(skills []inventory.Skill, sharedTerms []string) Finding {
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	if len(sharedTerms) > 6 {
		sharedTerms = sharedTerms[:6]
	}
	return Finding{ID: "overlap:" + strings.Join(lowerNames(names), ":"), Type: FindingOverlap, Severity: 2, Title: summarizeNames(names), Skills: skills, Reasons: []string{"shared domain keywords: " + strings.Join(sharedTerms, ", ")}}
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
	return sharedKeywordSets(overlapKeywords(a), overlapKeywords(b))
}

func overlapKeywords(skill inventory.Skill) map[string]bool {
	text := skill.Name + " " + skill.Description
	if strings.TrimSpace(skill.Description) == "" {
		text += " " + firstWords(skill.Body, 80)
	}
	return keywords(text)
}

func firstWords(text string, limit int) string {
	fields := strings.Fields(text)
	if len(fields) <= limit {
		return text
	}
	return strings.Join(fields[:limit], " ")
}

func sharedKeywordSets(ak, bk map[string]bool) []string {
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

func dropCorpusGenericKeywords(sets []map[string]bool) {
	if len(sets) < 6 {
		return
	}
	counts := map[string]int{}
	for _, set := range sets {
		for word := range set {
			counts[word]++
		}
	}
	for word, count := range counts {
		if count >= 6 && count*2 >= len(sets) {
			for _, set := range sets {
				delete(set, word)
			}
		}
	}
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

var keywordStopWords = map[string]bool{
	"about": true, "above": true, "accept": true, "accepted": true, "accepting": true, "accepts": true,
	"across": true, "action": true, "actions": true, "additional": true, "agent": true, "agents": true,
	"allow": true, "allowed": true, "allows": true, "also": true, "always": true, "anything": true,
	"area": true, "areas": true, "around": true, "ask": true, "asking": true, "asks": true,
	"assist": true, "assisted": true, "available": true, "based": true,
	"because": true, "before": true, "below": true, "body": true, "broad": true, "budget": true,
	"budgets": true, "build": true, "called": true, "calling": true, "case": true, "cases": true,
	"check": true, "cleanup": true, "code": true, "coding": true, "command": true, "commands": true,
	"common": true, "component": true, "components": true, "config": true, "configuration": true, "configured": true,
	"content": true, "context": true, "create": true, "creating": true, "current": true, "debug": true,
	"default": true, "describe": true,
	"description": true, "design": true, "develop": true, "development": true, "directory": true, "does": true,
	"domain": true, "edit": true, "enable": true, "enhance": true, "enough": true, "example": true,
	"examples": true, "every": true, "exceed": true, "feature": true, "file": true, "files": true,
	"find": true, "fix": true, "follow": true, "following": true, "follows": true, "from": true, "generate": true, "generated": true,
	"generic": true, "given": true, "handle": true, "handles": true, "help": true, "implement": true,
	"improve": true, "include": true, "includes": true, "including": true, "input": true, "inputs": true,
	"instance": true, "instances": true, "instruction": true, "instructions": true, "involving": true,
	"language": true, "local": true, "long": true, "manage": true, "management": true,
	"many": true, "material": true, "mention": true, "mentions": true, "modify": true, "must": true,
	"need": true, "needed": true, "needs": true, "option": true,
	"optional": true, "options": true, "optimize": true, "output": true, "outputs": true, "path": true,
	"paths": true, "plan": true, "plus": true, "product": true, "project": true, "read": true,
	"reference": true, "references": true, "refactor": true, "request": true, "requests": true, "result": true,
	"results": true, "return": true, "returns": true, "review": true, "said": true, "says": true,
	"scan": true, "section": true, "setting": true, "settings": true, "should": true, "skill": true,
	"skills": true, "someone": true, "specific": true,
	"summary": true, "summaries": true, "support": true, "task": true, "tasks": true, "term": true,
	"terms": true, "that": true, "their": true, "then": true, "thing": true, "things": true, "this": true,
	"text": true, "threshold": true, "token": true, "tokens": true, "tool": true, "tools": true,
	"trigger": true, "triggers": true, "uses": true, "using": true, "used": true, "user": true, "validate": true,
	"validated": true, "validates": true, "validation": true, "want": true, "wants": true,
	"when": true, "where": true,
	"with": true, "word": true, "words": true, "work": true, "workflow": true, "your": true,
}

func genericKeyword(word string) bool {
	return keywordStopWords[word]
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
