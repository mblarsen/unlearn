package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

const inventoryCacheKey = "dashboard-inventory-v1"

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
	if len(payload.Skills) == 0 && len(payload.Findings) == 0 {
		return nil, nil, fmt.Errorf("inventory cache is empty")
	}
	return payload.Skills, payload.Findings, nil
}

func boolInt(val bool) int {
	if val {
		return 1
	}
	return 0
}
