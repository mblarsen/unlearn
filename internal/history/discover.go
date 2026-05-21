package history

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultDiscoveryLimit = 200

// DiscoverPiJSONL returns a bounded list of Pi session JSONL files without
// reading their contents. History evidence extraction remains opt-in and is
// performed later by JSONLAdapter.
func DiscoverPiJSONL(home string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = DefaultDiscoveryLimit
	}
	roots := []string{
		filepath.Join(home, ".pi", "agent", "sessions"),
		filepath.Join(home, ".local", "share", "pi", "sessions"),
	}
	var paths []string
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if len(paths) >= limit {
				return filepath.SkipAll
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".jsonl" {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(paths) >= limit {
			break
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// DiscoverSQLite returns a bounded list of SQLite database files below the
// configured roots without opening or reading them. History evidence extraction
// remains opt-in and is performed later by SQLiteAdapter.
func DiscoverSQLite(roots []string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = DefaultDiscoveryLimit
	}
	seen := map[string]bool{}
	var paths []string
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if len(paths) >= limit {
				return filepath.SkipAll
			}
			if d.IsDir() {
				return nil
			}
			if !sqliteExtension(path) || seen[path] {
				return nil
			}
			paths = append(paths, path)
			seen[path] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(paths) >= limit {
			break
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func sqliteExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	default:
		return false
	}
}
