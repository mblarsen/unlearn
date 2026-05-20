package state

import (
	"database/sql"
	"time"

	"github.com/mblarsen/unlearn/internal/history"
)

type HistoryCacheStatus struct {
	Fresh bool
	MTime time.Time
}

func HistoryCacheStatusForSource(db *sql.DB, source string, mtime time.Time, ttl time.Duration, now time.Time) (HistoryCacheStatus, error) {
	var scannedRaw, mtimeRaw string
	err := db.QueryRow(`SELECT scanned_at, source_mtime FROM history_sources WHERE source = ?`, source).Scan(&scannedRaw, &mtimeRaw)
	if err == sql.ErrNoRows {
		return HistoryCacheStatus{MTime: mtime}, nil
	}
	if err != nil {
		return HistoryCacheStatus{}, err
	}
	scannedAt, err := time.Parse(time.RFC3339Nano, scannedRaw)
	if err != nil {
		return HistoryCacheStatus{MTime: mtime}, nil
	}
	cachedMTime, err := time.Parse(time.RFC3339Nano, mtimeRaw)
	if err != nil {
		return HistoryCacheStatus{MTime: mtime}, nil
	}
	return HistoryCacheStatus{Fresh: cachedMTime.Equal(mtime) && now.Sub(scannedAt) <= ttl, MTime: mtime}, nil
}

func LoadHistoryEvidence(db *sql.DB, source string) ([]history.Evidence, error) {
	rows, err := db.Query(`SELECT skill_name, grade, seen_at FROM history_evidence WHERE source = ?`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var evidence []history.Evidence
	for rows.Next() {
		var item history.Evidence
		var seenRaw string
		if err := rows.Scan(&item.SkillName, &item.Grade, &seenRaw); err != nil {
			return nil, err
		}
		item.Source = source
		if seenRaw != "" {
			item.SeenAt, _ = time.Parse(time.RFC3339Nano, seenRaw)
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func SaveHistoryEvidence(db *sql.DB, source string, mtime time.Time, evidence []history.Evidence) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM history_evidence WHERE source = ?`, source); err != nil {
		return err
	}
	scannedAt := time.Now().UTC().Format(time.RFC3339Nano)
	mtimeRaw := mtime.UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO history_sources(source, source_mtime, scanned_at) VALUES (?, ?, ?) ON CONFLICT(source) DO UPDATE SET source_mtime = excluded.source_mtime, scanned_at = excluded.scanned_at`, source, mtimeRaw, scannedAt); err != nil {
		return err
	}
	for _, item := range evidence {
		seenRaw := ""
		if !item.SeenAt.IsZero() {
			seenRaw = item.SeenAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err := tx.Exec(`INSERT INTO history_evidence(source, skill_name, grade, seen_at) VALUES (?, ?, ?, ?)`, source, item.SkillName, string(item.Grade), seenRaw); err != nil {
			return err
		}
	}
	return tx.Commit()
}
