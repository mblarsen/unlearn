package state

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Paths struct {
	BaseDir       string
	ConfigPath    string
	IndexPath     string
	QuarantineDir string
	LLMCacheDir   string
}

func DefaultPaths() (Paths, error) {
	stateHome, err := userStateDir()
	if err != nil {
		return Paths{}, err
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		BaseDir:       filepath.Join(stateHome, "unlearn"),
		ConfigPath:    filepath.Join(configHome, "unlearn", "config.toml"),
		IndexPath:     filepath.Join(stateHome, "unlearn", "index.db"),
		QuarantineDir: filepath.Join(stateHome, "unlearn", "quarantine"),
		LLMCacheDir:   filepath.Join(stateHome, "unlearn", "llm-cache"),
	}, nil
}

func userStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.BaseDir, filepath.Dir(p.ConfigPath), p.QuarantineDir, p.LLMCacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func OpenIndex(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS scans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scanned_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_instances (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  root TEXT NOT NULL,
  encountered_path TEXT NOT NULL,
  resolved_path TEXT,
  symlink INTEGER NOT NULL,
  broken INTEGER NOT NULL,
  content_hash TEXT,
  lower_tokens INTEGER NOT NULL,
  upper_tokens INTEGER NOT NULL,
  activation_risk TEXT NOT NULL,
  provenance TEXT NOT NULL,
  readonly INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS findings (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  severity INTEGER NOT NULL,
  title TEXT NOT NULL,
  reasons TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS history_sources (
  source TEXT PRIMARY KEY,
  source_mtime TEXT NOT NULL,
  scanned_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS history_evidence (
  source TEXT NOT NULL,
  skill_name TEXT NOT NULL,
  grade TEXT NOT NULL,
  seen_at TEXT NOT NULL,
  PRIMARY KEY (source, skill_name)
);
`)
	return err
}
