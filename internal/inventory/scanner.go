package inventory

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Scanner struct{}

func NewScanner() Scanner { return Scanner{} }

func (s Scanner) Scan(opts ScanOptions) (Report, error) {
	report := Report{}
	seen := map[string]bool{}
	for _, root := range opts.Roots {
		root = filepath.Clean(root)
		if seen[root] {
			continue
		}
		seen[root] = true
		rootReport := RootReport{Path: root}
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				report.Roots = append(report.Roots, rootReport)
				continue
			}
			rootReport.Error = err.Error()
			report.Roots = append(report.Roots, rootReport)
			continue
		}
		if !info.IsDir() {
			rootReport.Error = "not a directory"
			report.Roots = append(report.Roots, rootReport)
			continue
		}
		rootReport.Exists = true
		report.Roots = append(report.Roots, rootReport)
		skills, err := s.scanRoot(root)
		if err != nil {
			return report, err
		}
		report.Skills = append(report.Skills, skills...)
	}
	sort.Slice(report.Skills, func(i, j int) bool {
		if report.Skills[i].Name == report.Skills[j].Name {
			return report.Skills[i].EncounteredPath < report.Skills[j].EncounteredPath
		}
		return report.Skills[i].Name < report.Skills[j].Name
	})
	return report, nil
}

func (s Scanner) scanRoot(root string) ([]Skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, lstatErr := os.Lstat(path)
		if lstatErr != nil {
			continue
		}
		mode := info.Mode()
		isSymlink := mode&os.ModeSymlink != 0
		resolved, broken := resolve(path)
		if broken {
			skills = append(skills, Skill{
				ID:              stableID(path, resolved),
				Name:            entry.Name(),
				Kind:            KindSkillLike,
				Root:            root,
				EncounteredPath: path,
				ResolvedPath:    resolved,
				IsSymlink:       isSymlink,
				Broken:          true,
				ReadOnly:        true,
				ActivationRisk:  "unknown",
				Provenance:      inferProvenance(root, path, resolved, isSymlink),
				ScannedAt:       time.Now(),
			})
			continue
		}
		statInfo, err := os.Stat(path)
		if err != nil {
			continue
		}
		if statInfo.IsDir() {
			skillPath := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				skill, err := parseSkill(root, path, resolved, skillPath, KindDirectory, isSymlink, false)
				if err != nil {
					return nil, err
				}
				skills = append(skills, skill)
				continue
			}
			if looksSkillLikeDir(path) {
				skills = append(skills, unknownSkill(root, path, resolved, isSymlink, entry.Name(), true))
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			skill, err := parseSkill(root, path, resolved, path, KindMarkdown, isSymlink, false)
			if err != nil {
				return nil, err
			}
			skills = append(skills, skill)
		}
	}
	return skills, nil
}

func parseSkill(root, encountered, resolved, primary string, kind SkillKind, isSymlink, readOnly bool) (Skill, error) {
	data, err := os.ReadFile(primary)
	if err != nil {
		return Skill{}, err
	}
	front, body := ParseSkillMarkdown(data)
	name := front["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(encountered), filepath.Ext(encountered))
	}
	description := front["description"]
	refs := ExtractReferences(string(data))
	supportRefs := make([]SupportRef, 0, len(refs))
	upper := EstimateTokens(data)
	hashParts := [][]byte{data}
	var brokenRefs []string
	baseDir := encountered
	if kind == KindMarkdown {
		baseDir = filepath.Dir(encountered)
	}
	for _, ref := range refs {
		refPath := filepath.Join(baseDir, ref)
		refData, err := os.ReadFile(refPath)
		item := SupportRef{Mention: ref, Path: refPath}
		if err != nil {
			item.Broken = true
			brokenRefs = append(brokenRefs, ref)
		} else {
			item.Tokens = EstimateTokens(refData)
			upper += item.Tokens
			hashParts = append(hashParts, []byte(ref), refData)
		}
		supportRefs = append(supportRefs, item)
	}
	lower := EstimateTokens(data)
	return Skill{
		ID:              stableID(encountered, resolved),
		Name:            name,
		Description:     description,
		Kind:            kind,
		Root:            root,
		EncounteredPath: encountered,
		ResolvedPath:    resolved,
		IsSymlink:       isSymlink,
		ReadOnly:        readOnly,
		Frontmatter:     front,
		Body:            body,
		PrimaryPath:     primary,
		SupportRefs:     supportRefs,
		BrokenRefs:      brokenRefs,
		LowerTokens:     lower,
		UpperTokens:     upper,
		ContentHash:     ContentHash(hashParts...),
		ActivationRisk:  ActivationRisk(description, body),
		Provenance:      inferProvenance(root, encountered, resolved, isSymlink),
		ScannedAt:       time.Now(),
	}, nil
}

func unknownSkill(root, encountered, resolved string, isSymlink bool, name string, readOnly bool) Skill {
	return Skill{
		ID:              stableID(encountered, resolved),
		Name:            name,
		Kind:            KindSkillLike,
		Root:            root,
		EncounteredPath: encountered,
		ResolvedPath:    resolved,
		IsSymlink:       isSymlink,
		ReadOnly:        readOnly,
		ActivationRisk:  "unknown",
		Provenance:      inferProvenance(root, encountered, resolved, isSymlink),
		ScannedAt:       time.Now(),
	}
}

func resolve(path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", true
	}
	return resolved, false
}

func looksSkillLikeDir(path string) bool {
	found := false
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == path {
			return nil
		}
		if d.IsDir() && filepath.Dir(p) != path {
			return filepath.SkipDir
		}
		if strings.EqualFold(filepath.Ext(p), ".md") {
			found = true
		}
		return nil
	})
	return found
}

func stableID(encountered, resolved string) string {
	if resolved == "" {
		resolved = encountered
	}
	return ContentHash([]byte(encountered), []byte(resolved))[:16]
}

func inferProvenance(root, encountered, resolved string, isSymlink bool) string {
	parts := []string{}
	if isSymlink {
		parts = append(parts, fmt.Sprintf("symlink to %s", resolved))
	}
	if strings.Contains(encountered, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
		parts = append(parts, "npm package layout")
	}
	if _, err := os.Stat(filepath.Join(encountered, ".git")); err == nil {
		parts = append(parts, "local git checkout")
	}
	switch {
	case strings.Contains(root, filepath.Join(".pi", "agent", "skills")):
		parts = append(parts, "pi global skills root")
	case strings.Contains(root, filepath.Join(".agents", "skills")):
		parts = append(parts, "agents global skills root")
	case strings.Contains(root, filepath.Join(".codex", "skills")):
		parts = append(parts, "codex global skills root")
	case strings.Contains(root, filepath.Join("opencode", "skills")):
		parts = append(parts, "opencode global skills root")
	default:
		parts = append(parts, "user-provided root")
	}
	return strings.Join(parts, "; ")
}
