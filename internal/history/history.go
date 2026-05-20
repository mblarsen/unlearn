package history

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
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
	nameSet := map[string]bool{}
	for _, name := range skillNames {
		nameSet[strings.ToLower(name)] = true
	}
	best := map[string]EvidenceGrade{}
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines%500 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if opts.Progress != nil {
				opts.Progress(ScanProgress{Path: path, Lines: lines, Matches: len(best)})
			}
		}
		text := extractText(scanner.Bytes())
		lower := strings.ToLower(text)
		for name := range nameSet {
			if !strings.Contains(lower, name) {
				continue
			}
			grade := EvidenceWeak
			if strings.Contains(lower, "skill.md") || strings.Contains(lower, "use the "+name+" skill") || strings.Contains(lower, "using "+name) {
				grade = EvidenceStrong
			} else if strings.Contains(lower, "skills/") && strings.Contains(lower, name) {
				grade = EvidenceMedium
			}
			if rank(grade) < rank(best[name]) || best[name] == "" {
				best[name] = grade
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if opts.Progress != nil {
		opts.Progress(ScanProgress{Path: path, Lines: lines, Matches: len(best), Done: true})
	}
	var evidence []Evidence
	for name, grade := range best {
		evidence = append(evidence, Evidence{SkillName: name, Grade: grade, Source: path})
	}
	return evidence, nil
}

func extractText(line []byte) string {
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
