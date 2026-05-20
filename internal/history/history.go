package history

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	nameSet := map[string]bool{}
	for _, name := range skillNames {
		nameSet[strings.ToLower(name)] = true
	}
	best := map[string]EvidenceGrade{}
	reader := bufio.NewReader(f)
	lines := 0
	for {
		line, err := readHistoryLine(reader)
		if len(line) > 0 {
			lines++
			if lines%500 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if opts.Progress != nil {
					opts.Progress(ScanProgress{Path: path, Lines: lines, Matches: len(best)})
				}
			}
			text := extractText(line)
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
	if opts.Progress != nil {
		opts.Progress(ScanProgress{Path: path, Lines: lines, Matches: len(best), Done: true})
	}
	var evidence []Evidence
	for name, grade := range best {
		evidence = append(evidence, Evidence{SkillName: name, Grade: grade, Source: path})
	}
	return evidence, nil
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
