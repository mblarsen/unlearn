package state

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
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

// ReconcileMissingPaths retains the cache interface while delegating exact
// identity and snapshot transformation to inventorysnapshot.
func ReconcileMissingPaths(skills []inventory.Skill, findings []analysis.Finding) ([]inventory.Skill, []analysis.Finding, []inventory.Skill) {
	reconciled, missing := inventorysnapshot.ReconcileMissing(inventorysnapshot.Snapshot{Skills: skills, Findings: findings})
	return reconciled.Skills, reconciled.Findings, missing
}

// RemoveInventorySkills retains the state interface for callers while using the
// single exact-install identity rule owned by inventorysnapshot.
func RemoveInventorySkills(skills []inventory.Skill, findings []analysis.Finding, removed []inventory.Skill) ([]inventory.Skill, []analysis.Finding) {
	reconciled := inventorysnapshot.Remove(inventorysnapshot.Snapshot{Skills: skills, Findings: findings}, removed)
	return reconciled.Skills, reconciled.Findings
}

func boolInt(val bool) int {
	if val {
		return 1
	}
	return 0
}
