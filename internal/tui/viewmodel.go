package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

type findingSection struct {
	Type     analysis.FindingType
	Title    string
	Findings []analysis.Finding
}

type skillGroup struct {
	Name           string
	Skills         []inventory.Skill
	Representative inventory.Skill
}

func groupedFindings(findings []analysis.Finding) []findingSection {
	byType := map[analysis.FindingType][]analysis.Finding{}
	for _, finding := range findings {
		byType[finding.Type] = append(byType[finding.Type], finding)
	}
	var sections []findingSection
	for _, typ := range findingTypeOrder() {
		items := byType[typ]
		if len(items) == 0 {
			continue
		}
		sort.Slice(items, func(i, j int) bool {
			if len(items[i].Skills) != len(items[j].Skills) {
				return len(items[i].Skills) > len(items[j].Skills)
			}
			return items[i].Title < items[j].Title
		})
		sections = append(sections, findingSection{Type: typ, Title: findingTypeTitle(typ), Findings: items})
	}
	return sections
}

func findingTypeOrder() []analysis.FindingType {
	return []analysis.FindingType{
		analysis.FindingUnseen,
		analysis.FindingDuplicate,
		analysis.FindingConflict,
		analysis.FindingOverlap,
		analysis.FindingHighTokenCost,
		analysis.FindingBroadActivation,
		analysis.FindingBroken,
		analysis.FindingInactiveRoot,
	}
}

func findingTypeTitle(typ analysis.FindingType) string {
	switch typ {
	case analysis.FindingDuplicate:
		return "Duplicates"
	case analysis.FindingConflict:
		return "Conflicts"
	case analysis.FindingBroken:
		return "Broken links"
	case analysis.FindingOverlap:
		return "Overlaps"
	case analysis.FindingHighTokenCost:
		return "High token cost"
	case analysis.FindingBroadActivation:
		return "Broad activation risk"
	case analysis.FindingUnseen:
		return "Likely unused"
	case analysis.FindingInactiveRoot:
		return "Inactive harness roots"
	default:
		return string(typ)
	}
}

func groupedSkills(skills []inventory.Skill) []skillGroup {
	byName := map[string][]inventory.Skill{}
	for _, skill := range skills {
		key := strings.ToLower(strings.TrimSpace(skill.Name))
		byName[key] = append(byName[key], skill)
	}
	groups := make([]skillGroup, 0, len(byName))
	for _, items := range byName {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Root != items[j].Root {
				return items[i].Root < items[j].Root
			}
			return items[i].EncounteredPath < items[j].EncounteredPath
		})
		representative := items[0]
		for _, item := range items[1:] {
			if item.UpperTokens > representative.UpperTokens {
				representative = item
			}
		}
		groups = append(groups, skillGroup{Name: items[0].Name, Skills: items, Representative: representative})
	}
	sort.Slice(groups, func(i, j int) bool { return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name) })
	return groups
}

func findingTypeBadge(typ analysis.FindingType) string {
	switch typ {
	case analysis.FindingDuplicate:
		return "DUP"
	case analysis.FindingConflict:
		return "CONFLICT"
	case analysis.FindingBroken:
		return "BROKEN"
	case analysis.FindingOverlap:
		return "OVERLAP"
	case analysis.FindingHighTokenCost:
		return "TOKENS"
	case analysis.FindingBroadActivation:
		return "BROAD"
	case analysis.FindingUnseen:
		return "UNSEEN"
	default:
		return "FINDING"
	}
}

func selectedFindingIndex(sections []findingSection, cursor int) (int, int, bool) {
	idx := 0
	for sectionIdx, section := range sections {
		for findingIdx := range section.Findings {
			if idx == cursor {
				return sectionIdx, findingIdx, true
			}
			idx++
		}
	}
	return 0, 0, false
}

func findingCount(findings []analysis.Finding) int { return len(findings) }

func sectionSkillCount(section findingSection) int {
	seen := map[string]bool{}
	for _, finding := range section.Findings {
		for _, skill := range finding.Skills {
			seen[strings.ToLower(skill.Name)] = true
		}
	}
	return len(seen)
}

func findingInstallLabel(finding analysis.Finding) string {
	return installLabel(len(finding.Skills))
}

func installLabel(count int) string {
	if count == 1 {
		return "1 install"
	}
	return fmt.Sprintf("%d installs", count)
}

func tokenRange(skills []inventory.Skill) string {
	if len(skills) == 0 {
		return "0–0"
	}
	low := skills[0].LowerTokens
	high := skills[0].UpperTokens
	for _, skill := range skills[1:] {
		if skill.LowerTokens < low {
			low = skill.LowerTokens
		}
		if skill.UpperTokens > high {
			high = skill.UpperTokens
		}
	}
	if low == high {
		return compactNumber(low)
	}
	return fmt.Sprintf("%s–%s", compactNumber(low), compactNumber(high))
}

func compactNumber(value int) string {
	if value >= 1000 {
		whole := value / 1000
		frac := (value % 1000) / 100
		if frac == 0 {
			return fmt.Sprintf("%dk", whole)
		}
		return fmt.Sprintf("%d.%dk", whole, frac)
	}
	return fmt.Sprintf("%d", value)
}
