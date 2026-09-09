// Package inventorysnapshot owns exact-install identity and transformations of
// the inventory representations shown by the workbench and stored in SQLite.
package inventorysnapshot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// Snapshot is one coherent inventory representation.
type Snapshot struct {
	Skills   []inventory.Skill
	Findings []analysis.Finding
}

// Clone copies the snapshot slices so a failed operation cannot mutate its input.
func (s Snapshot) Clone() Snapshot {
	return Snapshot{
		Skills:   append([]inventory.Skill(nil), s.Skills...),
		Findings: cloneFindings(s.Findings),
	}
}

// SameInstall applies the one exact-install identity rule used by all snapshot
// reconciliation. IDs are strong hints, but canonical encountered paths remain
// authoritative when separately loaded representations carry different IDs.
func SameInstall(a, b inventory.Skill) bool {
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	aPath, bPath := installPath(a), installPath(b)
	if aPath != "" && bPath != "" {
		return aPath == bPath
	}
	if aPath != "" || bPath != "" {
		return false
	}
	return strings.EqualFold(a.Name, b.Name) && cleanPath(a.Root) == cleanPath(b.Root)
}

// Remove removes exact installs from skills and every finding, then prunes
// findings that no longer have enough members to be meaningful.
func Remove(snapshot Snapshot, removed []inventory.Skill) Snapshot {
	out := snapshot.Clone()
	for _, target := range removed {
		out.Skills = removeSkills(out.Skills, target)
		next := make([]analysis.Finding, 0, len(out.Findings))
		for _, finding := range out.Findings {
			finding.Skills = removeSkills(finding.Skills, target)
			if keepFinding(finding) {
				next = append(next, finding)
			}
		}
		out.Findings = next
	}
	return out
}

// Rename replaces an exact install in the skill inventory and removes its old
// finding memberships, which are no longer trustworthy after name/content change.
func Rename(snapshot Snapshot, old inventory.Skill, newName, newPath string) (Snapshot, inventory.Skill) {
	out := Remove(snapshot, []inventory.Skill{old})
	renamed := renamedSkill(old, newName, newPath)
	out.Skills = append(out.Skills, renamed)
	return out, renamed
}

// Add inserts an install unless the snapshot already contains that exact install.
func Add(snapshot Snapshot, skill inventory.Skill) Snapshot {
	out := snapshot.Clone()
	for i := range out.Skills {
		if SameInstall(out.Skills[i], skill) {
			out.Skills[i] = skill
			return out
		}
	}
	out.Skills = append(out.Skills, skill)
	return out
}

// ReconcileMissing removes cached encountered paths that no longer exist.
// Lstat preserves broken symlinks and treats permission errors as present.
func ReconcileMissing(snapshot Snapshot) (Snapshot, []inventory.Skill) {
	var missing []inventory.Skill
	for _, skill := range snapshot.Skills {
		if skill.EncounteredPath == "" {
			continue
		}
		if _, err := os.Lstat(skill.EncounteredPath); errors.Is(err, os.ErrNotExist) {
			missing = append(missing, skill)
		}
	}
	return Remove(snapshot, missing), missing
}

func installPath(skill inventory.Skill) string {
	if skill.EncounteredPath != "" {
		return cleanPath(skill.EncounteredPath)
	}
	if skill.PrimaryPath == "" {
		return ""
	}
	path := skill.PrimaryPath
	if skill.Kind == inventory.KindDirectory && filepath.Base(path) == "SKILL.md" {
		path = filepath.Dir(path)
	}
	return cleanPath(path)
}

func cleanPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func renamedSkill(old inventory.Skill, newName, newPath string) inventory.Skill {
	renamed := old
	renamed.ID = "" // The old path-derived ID must not identify the renamed install.
	renamed.Name = newName
	renamed.EncounteredPath = newPath
	if old.ResolvedPath != "" {
		renamed.ResolvedPath = newPath
	}
	if old.PrimaryPath != "" {
		if rel, err := filepath.Rel(old.EncounteredPath, old.PrimaryPath); err == nil {
			renamed.PrimaryPath = filepath.Join(newPath, rel)
		}
	}
	if old.Frontmatter != nil {
		renamed.Frontmatter = make(map[string]string, len(old.Frontmatter))
		for key, value := range old.Frontmatter {
			renamed.Frontmatter[key] = value
		}
		renamed.Frontmatter["name"] = newName
	}
	return renamed
}

func removeSkills(skills []inventory.Skill, target inventory.Skill) []inventory.Skill {
	out := make([]inventory.Skill, 0, len(skills))
	for _, skill := range skills {
		if !SameInstall(skill, target) {
			out = append(out, skill)
		}
	}
	return out
}

func keepFinding(finding analysis.Finding) bool {
	switch finding.Type {
	case analysis.FindingDuplicate, analysis.FindingConflict, analysis.FindingOverlap:
		return len(finding.Skills) > 1
	default:
		return len(finding.Skills) > 0
	}
}

func cloneFindings(findings []analysis.Finding) []analysis.Finding {
	out := append([]analysis.Finding(nil), findings...)
	for i := range out {
		out[i].Skills = append([]inventory.Skill(nil), out[i].Skills...)
	}
	return out
}
