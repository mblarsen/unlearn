package state

import "database/sql"

// HistorySourceInactive records scan eligibility separately from derived evidence.
// Inactive references retain their evidence and incomplete-coverage semantics.
func HistorySourceInactive(db *sql.DB, source string) (bool, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inactive_history_sources (source TEXT PRIMARY KEY)`); err != nil {
		return false, err
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM inactive_history_sources WHERE source = ?`, source).Scan(&count)
	return count != 0, err
}

// SetHistorySourceInactive returns whether this call changed source eligibility.
func SetHistorySourceInactive(db *sql.DB, source string, inactive bool) (bool, error) {
	if _, err := HistorySourceInactive(db, source); err != nil {
		return false, err
	}
	query := `DELETE FROM inactive_history_sources WHERE source = ?`
	if inactive {
		query = `INSERT OR IGNORE INTO inactive_history_sources(source) VALUES (?)`
	}
	result, err := db.Exec(query, source)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count != 0, err
}
