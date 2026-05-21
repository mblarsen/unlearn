package inventory

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
		ownership, known := opts.RootOwnerships[root]
		skills, err := s.scanRoot(root, ownership, known)
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

func (s Scanner) scanRoot(root string, ownership RootOwnership, rootKnown bool) ([]Skill, error) {
	parser := newSkillInstallParser()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, entry := range entries {
		if ignoreRootEntry(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, lstatErr := os.Lstat(path)
		if lstatErr != nil {
			continue
		}
		mode := info.Mode()
		isSymlink := mode&os.ModeSymlink != 0
		resolved, broken := resolve(path)
		install := skillInstall{
			Root:            root,
			EncounteredPath: path,
			ResolvedPath:    resolved,
			Kind:            KindSkillLike,
			IsSymlink:       isSymlink,
			Ownership:       ownership,
			RootKnown:       rootKnown,
		}
		if broken {
			skills = append(skills, parser.Broken(install, entry.Name()))
			continue
		}
		statInfo, err := os.Stat(path)
		if err != nil {
			continue
		}
		if statInfo.IsDir() {
			skillPath := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				install.Kind = KindDirectory
				install.PrimaryPath = skillPath
				skill, err := parser.Parse(install)
				if err != nil {
					return nil, err
				}
				skills = append(skills, skill)
				continue
			}
			if looksSkillLikeDir(path) {
				install.ReadOnly = true
				skills = append(skills, parser.Unknown(install, entry.Name()))
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			install.Kind = KindMarkdown
			install.PrimaryPath = path
			skill, err := parser.Parse(install)
			if err != nil {
				return nil, err
			}
			skills = append(skills, skill)
		}
	}
	return skills, nil
}

func ignoreRootEntry(name string) bool {
	return name == ".system"
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
