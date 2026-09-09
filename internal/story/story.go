// Package story builds a truthful, read-only account of one logical skill.
package story

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/inventory"
)

// Coverage states whether absence of derived history evidence is meaningful.
type Coverage string

const (
	CoverageUnknown    Coverage = "unknown"
	CoverageIncomplete Coverage = "incomplete"
	CoverageComplete   Coverage = "complete"
)

// Story contains observed facts and explicit evidence limits for one skill name.
type Story struct {
	Name               string
	Origin             string
	InstallDate        string
	UpstreamBaseline   string
	ModificationStatus string
	CopyComparisonNote string
	Installs           []Install
	Comparisons        []Comparison
	Usage              Usage
}

// Install describes one exact path from the local inventory.
type Install struct {
	Path           string
	ResolvedPath   string
	Symlink        bool
	Kind           string
	SourceEvidence string
	AgentAccess    string
	ContentHash    string
}

// Comparison describes observed differences between two installed copies.
type Comparison struct {
	LeftPath    string
	RightPath   string
	Differences []string
}

// Usage summarizes opt-in, derived history evidence at skill-name level.
type Usage struct {
	Status      string
	Coverage    string
	SourceCount int
	LastSeen    time.Time
	Attribution string
	Limit       string
}

// Build creates a deterministic story without reading files or history.
func Build(skills []inventory.Skill, coverage Coverage) Story {
	items := append([]inventory.Skill(nil), skills...)
	sort.Slice(items, func(i, j int) bool {
		return installPath(items[i]) < installPath(items[j])
	})

	result := Story{
		Origin:             "unknown; the inventory has no verified original source",
		InstallDate:        "unknown; filesystem timestamps are not install dates",
		UpstreamBaseline:   "unknown; no upstream baseline was fetched or recorded",
		ModificationStatus: "unknown; modification requires an upstream baseline",
		CopyComparisonNote: "Comparisons use installed copies only. They do not establish an upstream version.",
		Usage:              buildUsage(items, coverage),
	}
	if len(items) > 0 {
		result.Name = items[0].Name
	}
	for _, skill := range items {
		result.Installs = append(result.Installs, buildInstall(skill))
	}
	if len(items) < 2 {
		result.CopyComparisonNote = "No other installed copy is available for comparison. Upstream baseline remains unknown."
		return result
	}
	for i := 1; i < len(items); i++ {
		result.Comparisons = append(result.Comparisons, compare(items[0], items[i]))
	}
	return result
}

func buildInstall(skill inventory.Skill) Install {
	source := strings.TrimSpace(skill.Provenance)
	if source == "" {
		source = "no source-layout evidence observed"
	}
	return Install{
		Path:           installPath(skill),
		ResolvedPath:   strings.TrimSpace(skill.ResolvedPath),
		Symlink:        skill.IsSymlink,
		Kind:           string(skill.Kind),
		SourceEvidence: source,
		AgentAccess:    agentAccess(skill),
		ContentHash:    strings.TrimSpace(skill.ContentHash),
	}
}

func agentAccess(skill inventory.Skill) string {
	active := sortedUnique(skill.ActiveAgents)
	inactive := sortedUnique(skill.InactiveAgents)
	var parts []string
	if len(active) > 0 {
		parts = append(parts, "active harness access: "+strings.Join(active, ", "))
	}
	if len(inactive) > 0 {
		parts = append(parts, "inactive harness root: "+strings.Join(inactive, ", "))
	}
	if len(parts) == 0 {
		return "unknown; this root has no recorded harness ownership"
	}
	return strings.Join(parts, "; ")
}

func compare(left, right inventory.Skill) Comparison {
	result := Comparison{LeftPath: installPath(left), RightPath: installPath(right)}
	if samePhysicalInstall(left, right) {
		result.Differences = append(result.Differences, "same resolved filesystem object")
	}
	if left.Body != right.Body {
		result.Differences = append(result.Differences, "SKILL.md body differs")
	}
	result.Differences = append(result.Differences, compareMetadata(left.Frontmatter, right.Frontmatter, result.LeftPath, result.RightPath)...)
	if supportSignature(left.SupportRefs) != supportSignature(right.SupportRefs) {
		result.Differences = append(result.Differences, "support references differ")
	}
	switch {
	case left.ContentHash == "" || right.ContentHash == "":
		result.Differences = append(result.Differences, "effective content comparison unknown; one or both hashes are unavailable")
	case left.ContentHash == right.ContentHash:
		result.Differences = append(result.Differences, "effective content is identical")
	default:
		result.Differences = append(result.Differences, "effective content differs")
	}
	return result
}

func compareMetadata(left, right map[string]string, leftPath, rightPath string) []string {
	keys := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var differences []string
	for _, key := range keys {
		leftValue, leftOK := left[key]
		rightValue, rightOK := right[key]
		switch {
		case leftOK && !rightOK:
			differences = append(differences, fmt.Sprintf("metadata %s only in %s", key, leftPath))
		case !leftOK && rightOK:
			differences = append(differences, fmt.Sprintf("metadata %s only in %s", key, rightPath))
		case leftValue != rightValue:
			differences = append(differences, fmt.Sprintf("metadata %s differs", key))
		}
	}
	return differences
}

func buildUsage(skills []inventory.Skill, coverage Coverage) Usage {
	result := Usage{
		Coverage:    coverageText(coverage),
		Attribution: "Derived usage evidence applies to the skill name, not a specific installed copy.",
		Limit:       "Not observed does not mean unused. The scan only covers configured local history sources.",
	}
	best := ""
	sources := map[string]bool{}
	for _, skill := range skills {
		if usageRank(skill.HistoryEvidence) < usageRank(best) {
			best = skill.HistoryEvidence
		}
		for _, source := range skill.HistorySources {
			sources[source] = true
		}
		if skill.HistoryLastSeenAt.After(result.LastSeen) {
			result.LastSeen = skill.HistoryLastSeenAt
		}
	}
	result.SourceCount = len(sources)
	switch best {
	case "strong", "medium":
		result.Status = best + " derived evidence"
	case "weak":
		result.Status = "weak mention only; not counted as observed use"
	default:
		if coverage == CoverageComplete {
			result.Status = "not observed"
		} else {
			result.Status = "unknown; evidence coverage cannot support an absence claim"
		}
	}
	return result
}

func coverageText(coverage Coverage) string {
	switch coverage {
	case CoverageComplete:
		return "complete for configured history sources"
	case CoverageIncomplete:
		return "incomplete; one or more configured history sources were unavailable"
	default:
		return "unknown; opt-in history evidence is unavailable"
	}
}

func installPath(skill inventory.Skill) string {
	if strings.TrimSpace(skill.EncounteredPath) != "" {
		return skill.EncounteredPath
	}
	return skill.PrimaryPath
}

func samePhysicalInstall(left, right inventory.Skill) bool {
	return left.ResolvedPath != "" && right.ResolvedPath != "" && left.ResolvedPath == right.ResolvedPath
}

func supportSignature(refs []inventory.SupportRef) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, fmt.Sprintf("%s:%t:%d", ref.Mention, ref.Broken, ref.Tokens))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func usageRank(grade string) int {
	switch strings.ToLower(strings.TrimSpace(grade)) {
	case "strong":
		return 1
	case "medium":
		return 2
	case "weak":
		return 3
	default:
		return 99
	}
}
