package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// inventoryCacheKey versions cached analysis semantics as well as payload shape.
// v3 adds preview-only LLM-assisted skill-quality findings.
const inventoryCacheKey = "dashboard-inventory-v3"

type inventoryCachePayload struct {
	Skills   []inventory.Skill  `json:"skills"`
	Findings []analysis.Finding `json:"findings"`
}

func ReplaceIndex(db *sql.DB, skills []inventory.Skill, findings []analysis.Finding) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM skill_instances"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM findings"); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec("INSERT INTO scans(scanned_at) VALUES (?)", now); err != nil {
		return err
	}
	for _, skill := range skills {
		if _, err := tx.Exec(`INSERT INTO skill_instances(id, name, kind, root, encountered_path, resolved_path, symlink, broken, content_hash, lower_tokens, upper_tokens, activation_risk, provenance, readonly)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, skill.ID, skill.Name, string(skill.Kind), skill.Root, skill.EncounteredPath, skill.ResolvedPath, boolInt(skill.IsSymlink), boolInt(skill.Broken), skill.ContentHash, skill.LowerTokens, skill.UpperTokens, skill.ActivationRisk, skill.Provenance, boolInt(skill.ReadOnly)); err != nil {
			return err
		}
	}
	for _, finding := range findings {
		if _, err := tx.Exec(`INSERT INTO findings(id, type, severity, title, reasons) VALUES (?, ?, ?, ?, ?)`, finding.ID, string(finding.Type), finding.Severity, finding.Title, strings.Join(finding.Reasons, "\n")); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(inventoryCachePayload{Skills: skills, Findings: findings})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO inventory_cache(key, payload, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`, inventoryCacheKey, string(payload), now); err != nil {
		return err
	}
	return tx.Commit()
}

func LoadInventoryCache(db *sql.DB) ([]inventory.Skill, []analysis.Finding, error) {
	var raw string
	if err := db.QueryRow(`SELECT payload FROM inventory_cache WHERE key = ?`, inventoryCacheKey).Scan(&raw); err != nil {
		return nil, nil, err
	}
	var payload inventoryCachePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, nil, err
	}
	return payload.Skills, payload.Findings, nil
}

// ReconcileMissingPaths removes cached installs whose encountered paths were
// deleted outside unlearn. Lstat keeps broken symlinks in inventory while
// permission and other filesystem errors remain visible instead of being
// mistaken for absence.
func ReconcileMissingPaths(skills []inventory.Skill, findings []analysis.Finding) ([]inventory.Skill, []analysis.Finding, []inventory.Skill) {
	var missing []inventory.Skill
	for _, skill := range skills {
		if skill.EncounteredPath == "" {
			continue
		}
		if _, err := os.Lstat(skill.EncounteredPath); errors.Is(err, os.ErrNotExist) {
			missing = append(missing, skill)
		}
	}
	remainingSkills, remainingFindings := RemoveInventorySkills(skills, findings, missing)
	return remainingSkills, remainingFindings, missing
}

// RemoveInventorySkills removes exact installs and prunes findings that no
// longer have enough members to be meaningful.
func RemoveInventorySkills(skills []inventory.Skill, findings []analysis.Finding, removed []inventory.Skill) ([]inventory.Skill, []analysis.Finding) {
	remainingSkills := append([]inventory.Skill(nil), skills...)
	remainingFindings := append([]analysis.Finding(nil), findings...)
	for _, skill := range removed {
		remainingSkills = removeSkill(remainingSkills, skill)
		nextFindings := make([]analysis.Finding, 0, len(remainingFindings))
		for _, finding := range remainingFindings {
			finding.Skills = removeSkill(finding.Skills, skill)
			if keepFinding(finding) {
				nextFindings = append(nextFindings, finding)
			}
		}
		remainingFindings = nextFindings
	}
	return remainingSkills, remainingFindings
}

func removeSkill(skills []inventory.Skill, removed inventory.Skill) []inventory.Skill {
	out := make([]inventory.Skill, 0, len(skills))
	for _, skill := range skills {
		if !sameSkillInstall(skill, removed) {
			out = append(out, skill)
		}
	}
	return out
}

func sameSkillInstall(a, b inventory.Skill) bool {
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	if a.EncounteredPath != "" && b.EncounteredPath != "" {
		return a.EncounteredPath == b.EncounteredPath
	}
	return a.Name == b.Name && a.Root == b.Root
}

func keepFinding(finding analysis.Finding) bool {
	switch finding.Type {
	case analysis.FindingDuplicate, analysis.FindingConflict, analysis.FindingOverlap:
		return len(finding.Skills) > 1
	default:
		return len(finding.Skills) > 0
	}
}

func boolInt(val bool) int {
	if val {
		return 1
	}
	return 0
}
