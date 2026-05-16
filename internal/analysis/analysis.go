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
	for _, skill := range skills {
		if skill.Broken || len(skill.BrokenRefs) > 0 {
			reasons := []string{}
			if skill.Broken {
				reasons = append(reasons, "encountered path is a broken symlink or cannot be resolved")
			}
			for _, ref := range skill.BrokenRefs {
				reasons = append(reasons, "missing referenced support file: "+ref)
			}
			findings = append(findings, Finding{ID: "broken:" + skill.ID, Type: FindingBroken, Severity: 1, Title: "Broken symlink/reference: " + skill.Name, Skills: []inventory.Skill{skill}, Reasons: reasons})
		}
		if skill.UpperTokens >= opts.HighTokenLimit {
			findings = append(findings, Finding{ID: "tokens:" + skill.ID, Type: FindingHighTokenCost, Severity: 3, Title: "High token cost: " + skill.Name, Skills: []inventory.Skill{skill}, Reasons: []string{fmt.Sprintf("estimated token range %d-%d exceeds %d", skill.LowerTokens, skill.UpperTokens, opts.HighTokenLimit)}})
		}
		if skill.ActivationRisk == "high" {
			findings = append(findings, Finding{ID: "activation:" + skill.ID, Type: FindingBroadActivation, Severity: 3, Title: "Broad activation risk: " + skill.Name, Skills: []inventory.Skill{skill}, Reasons: []string{"description/body contains broad trigger language"}})
		}
		if opts.UsageEvidence != nil {
			grade := opts.UsageEvidence[skill.Name]
			if grade == "" || grade == "weak" {
				findings = append(findings, Finding{ID: "unseen:" + skill.ID, Type: FindingUnseen, Severity: 4, Title: "Unseen skill: " + skill.Name, Skills: []inventory.Skill{skill}, Reasons: []string{"no strong or medium invocation evidence found in opted-in history"}})
			}
		}
	}
	SortFindings(findings)
	return findings
}

func duplicatesAndConflicts(skills []inventory.Skill) []Finding {
	byName := map[string][]inventory.Skill{}
	for _, skill := range skills {
		byName[strings.ToLower(skill.Name)] = append(byName[strings.ToLower(skill.Name)], skill)
	}
	var findings []Finding
	for name, group := range byName {
		if len(group) < 2 {
			continue
		}
		byHash := map[string][]inventory.Skill{}
		for _, skill := range group {
			byHash[skill.ContentHash] = append(byHash[skill.ContentHash], skill)
		}
		if len(byHash) == 1 {
			findings = append(findings, Finding{ID: "duplicate:" + name, Type: FindingDuplicate, Severity: 1, Title: "Duplicate skill: " + group[0].Name, Skills: group, Reasons: []string{"same skill name and identical effective content"}})
			continue
		}
		findings = append(findings, Finding{ID: "conflict:" + name, Type: FindingConflict, Severity: 1, Title: "Conflicting skill: " + group[0].Name, Skills: group, Reasons: []string{"same skill name but different effective content"}})
	}
	return findings
}

func overlaps(skills []inventory.Skill) []Finding {
	var findings []Finding
	for i := 0; i < len(skills); i++ {
		for j := i + 1; j < len(skills); j++ {
			a, b := skills[i], skills[j]
			if strings.EqualFold(a.Name, b.Name) {
				continue
			}
			shared := sharedKeywords(a, b)
			if len(shared) >= 3 {
				idNames := []string{strings.ToLower(a.Name), strings.ToLower(b.Name)}
				sort.Strings(idNames)
				findings = append(findings, Finding{ID: "overlap:" + strings.Join(idNames, ":"), Type: FindingOverlap, Severity: 2, Title: "Overlapping skills: " + a.Name + " / " + b.Name, Skills: []inventory.Skill{a, b}, Reasons: []string{"shared purpose keywords: " + strings.Join(shared, ", ")}})
			}
		}
	}
	return findings
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
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "when": true, "from": true, "your": true, "you": true, "use": true, "skill": true, "skills": true, "agent": true, "agents": true, "must": true, "any": true, "all": true}
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r < 'a' || r > 'z' })
	out := map[string]bool{}
	for _, field := range fields {
		if len(field) < 5 || stop[field] {
			continue
		}
		out[field] = true
	}
	return out
}

func SortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity < findings[j].Severity
		}
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
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
