package history

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type EvidenceGrade string

const (
	EvidenceStrong EvidenceGrade = "strong"
	EvidenceMedium EvidenceGrade = "medium"
	EvidenceWeak   EvidenceGrade = "weak"
)

type Evidence struct {
	SkillName string
	Grade     EvidenceGrade
	Source    string
	SeenAt    time.Time
}

type Adapter interface {
	Scan(path string, skillNames []string) ([]Evidence, error)
}

type JSONLAdapter struct{}

type ScanProgress struct {
	Path    string
	Lines   int
	Matches int
	Done    bool
}

const maxHistoryLineBytes = 16 * 1024 * 1024

type ScanOptions struct {
	Context  context.Context
	Progress func(ScanProgress)
}

func (adapter JSONLAdapter) Scan(path string, skillNames []string) ([]Evidence, error) {
	return adapter.ScanWithOptions(path, skillNames, ScanOptions{})
}

func (JSONLAdapter) ScanWithOptions(path string, skillNames []string, opts ScanOptions) ([]Evidence, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	matcher := newMatcher(skillNames)
	fallbackSeenAt := fileModTime(path)
	reader := bufio.NewReader(f)
	lines := 0
	reportProgress(opts, ScanProgress{Path: path, Lines: lines, Matches: matcher.MatchCount()})
	for {
		line, err := readHistoryLine(reader)
		if len(line) > 0 {
			lines++
			if lines%500 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				reportProgress(opts, ScanProgress{Path: path, Lines: lines, Matches: matcher.MatchCount()})
			}
			seenAt := extractSeenAt(line)
			if seenAt.IsZero() {
				seenAt = fallbackSeenAt
			}
			matcher.Observe(extractText(line), seenAt)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reportProgress(opts, ScanProgress{Path: path, Lines: lines, Matches: matcher.MatchCount(), Done: true})
	return matcher.Evidence(path), nil
}

type SQLiteAdapter struct {
	RowLimit int
}

const DefaultSQLiteRowLimit = 5000

func (a SQLiteAdapter) Scan(path string, skillNames []string) ([]Evidence, error) {
	return a.ScanWithOptions(path, skillNames, ScanOptions{})
}

func (a SQLiteAdapter) ScanWithOptions(path string, skillNames []string, opts ScanOptions) ([]Evidence, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	limit := a.RowLimit
	if limit <= 0 {
		limit = DefaultSQLiteRowLimit
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tables, err := sqliteTables(db)
	if err != nil {
		return nil, err
	}
	matcher := newMatcher(skillNames)
	seenAt := fileModTime(path)
	rowsScanned := 0
	reportProgress(opts, ScanProgress{Path: path, Lines: rowsScanned, Matches: matcher.MatchCount()})
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		columns, err := sqliteTextColumns(db, table)
		if err != nil {
			return nil, err
		}
		if len(columns) == 0 {
			continue
		}
		count, err := scanSQLiteTable(ctx, db, table, columns, limit, matcher, seenAt, func(rows int) {
			rowsScanned += rows
			reportProgress(opts, ScanProgress{Path: path, Lines: rowsScanned, Matches: matcher.MatchCount()})
		})
		rowsScanned += count
		if err != nil {
			return nil, err
		}
	}
	reportProgress(opts, ScanProgress{Path: path, Lines: rowsScanned, Matches: matcher.MatchCount(), Done: true})
	return matcher.Evidence(path), nil
}

func reportProgress(opts ScanOptions, progress ScanProgress) {
	if opts.Progress != nil {
		opts.Progress(progress)
	}
}

func sqliteReadOnlyDSN(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	return u.String()
}

func sqliteTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func sqliteTextColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + quoteSQLiteIdent(table) + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		if sqliteTypeLooksText(typ) {
			columns = append(columns, name)
		}
	}
	return columns, rows.Err()
}

func sqliteTypeLooksText(typ string) bool {
	upper := strings.ToUpper(strings.TrimSpace(typ))
	if upper == "" {
		return true
	}
	for _, marker := range []string{"CHAR", "CLOB", "TEXT", "VARCHAR", "JSON"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func scanSQLiteTable(ctx context.Context, db *sql.DB, table string, columns []string, limit int, matcher *evidenceMatcher, seenAt time.Time, progress func(rows int)) (int, error) {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, quoteSQLiteIdent(column))
	}
	query := fmt.Sprintf("SELECT %s FROM %s LIMIT ?", strings.Join(quoted, ", "), quoteSQLiteIdent(table))
	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	values := make([]sql.NullString, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	scanned := 0
	pendingProgress := 0
	for rows.Next() {
		if scanned%500 == 0 {
			if err := ctx.Err(); err != nil {
				return scanned, err
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return scanned, err
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			if value.Valid {
				parts = append(parts, value.String)
			}
		}
		matcher.Observe(strings.Join(parts, " "), seenAt)
		scanned++
		pendingProgress++
		if pendingProgress == 500 {
			progress(pendingProgress)
			pendingProgress = 0
		}
	}
	return pendingProgress, rows.Err()
}

func quoteSQLiteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

type evidenceMatcher struct {
	names    map[string]bool
	best     map[string]EvidenceGrade
	lastSeen map[string]time.Time
}

func newMatcher(skillNames []string) *evidenceMatcher {
	names := map[string]bool{}
	for _, name := range skillNames {
		names[strings.ToLower(name)] = true
	}
	return &evidenceMatcher{names: names, best: map[string]EvidenceGrade{}, lastSeen: map[string]time.Time{}}
}

func (m *evidenceMatcher) Observe(text string, seenAt time.Time) {
	lower := strings.ToLower(text)
	for name := range m.names {
		if !strings.Contains(lower, name) {
			continue
		}
		grade := gradeEvidence(lower, name)
		if rank(grade) < rank(m.best[name]) || m.best[name] == "" {
			m.best[name] = grade
		}
		if grade != EvidenceWeak && seenAt.After(m.lastSeen[name]) {
			m.lastSeen[name] = seenAt
		}
	}
}

func (m *evidenceMatcher) Evidence(source string) []Evidence {
	evidence := make([]Evidence, 0, len(m.best))
	for name, grade := range m.best {
		evidence = append(evidence, Evidence{SkillName: name, Grade: grade, Source: source, SeenAt: m.lastSeen[name]})
	}
	return evidence
}

func (m *evidenceMatcher) MatchCount() int {
	return len(m.best)
}

func gradeEvidence(lower, name string) EvidenceGrade {
	if strings.Contains(lower, "skill.md") || strings.Contains(lower, "use the "+name+" skill") || strings.Contains(lower, "using "+name) {
		return EvidenceStrong
	}
	if strings.Contains(lower, "skills/") && strings.Contains(lower, name) {
		return EvidenceMedium
	}
	return EvidenceWeak
}

func readHistoryLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, prefix, err := reader.ReadLine()
		if len(part) > 0 && len(line) < maxHistoryLineBytes {
			remaining := maxHistoryLineBytes - len(line)
			line = append(line, part[:min(len(part), remaining)]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, nil
			}
			return line, err
		}
		if !prefix {
			return line, nil
		}
	}
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func extractSeenAt(line []byte) time.Time {
	line = bytes.TrimSpace(line)
	var value any
	if err := json.Unmarshal(line, &value); err != nil {
		return time.Time{}
	}
	return findTime(value)
}

func findTime(value any) time.Time {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"timestamp", "time", "created_at", "createdAt", "date"} {
			if parsed := parseTimeValue(v[key]); !parsed.IsZero() {
				return parsed
			}
		}
		for _, item := range v {
			if parsed := findTime(item); !parsed.IsZero() {
				return parsed
			}
		}
	case []any:
		for _, item := range v {
			if parsed := findTime(item); !parsed.IsZero() {
				return parsed
			}
		}
	}
	return time.Time{}
}

func parseTimeValue(value any) time.Time {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func extractText(line []byte) string {
	line = bytes.TrimSpace(line)
	var value any
	if err := json.Unmarshal(line, &value); err != nil {
		return string(line)
	}
	return flatten(value)
}

func flatten(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, flatten(item))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, flatten(item))
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func rank(grade EvidenceGrade) int {
	switch grade {
	case EvidenceStrong:
		return 1
	case EvidenceMedium:
		return 2
	case EvidenceWeak:
		return 3
	default:
		return 99
	}
}
