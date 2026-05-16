package state

import (
	"database/sql"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

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
	if _, err := tx.Exec("INSERT INTO scans(scanned_at) VALUES (?)", time.Now().UTC().Format(time.RFC3339)); err != nil {
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
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
