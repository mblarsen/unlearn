package actions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

var ErrWritePermissionRequired = errors.New("write permission required for skill root")
var ErrConfirmationRequired = errors.New("confirmation required")
var ErrRenameNameRequired = errors.New("rename requires a new name")

type Manager struct {
	Config        config.Config
	QuarantineDir string
}

type Result struct {
	Skills []inventory.Skill
	Paths  []string
}

type DeleteConfirmation struct {
	TypedName  string
	BatchToken string
}

func BatchDeleteConfirmation(skills []inventory.Skill) string {
	return fmt.Sprintf("delete %d selected installs", len(skills))
}

func (m Manager) QuarantineSelected(skills []inventory.Skill, confirm bool) (Result, error) {
	if !confirm {
		return Result{}, ErrConfirmationRequired
	}
	if missing, ok := FirstMissingWrite(m.Config, skills); ok {
		return Result{}, fmt.Errorf("%w: %s", ErrWritePermissionRequired, missing.Root)
	}
	result := Result{Skills: append([]inventory.Skill(nil), skills...)}
	for _, skill := range skills {
		dest, err := m.Quarantine(skill, true)
		if err != nil {
			return result, err
		}
		result.Paths = append(result.Paths, dest)
	}
	return result, nil
}

func (m Manager) DeleteSelected(skills []inventory.Skill, confirmation DeleteConfirmation) (Result, error) {
	if len(skills) == 0 {
		return Result{}, ErrNoInstallSelected
	}
	if missing, ok := FirstMissingWrite(m.Config, skills); ok {
		return Result{}, fmt.Errorf("%w: %s", ErrWritePermissionRequired, missing.Root)
	}
	if len(skills) == 1 {
		return m.deleteSingleSelected(skills[0], confirmation.TypedName)
	}
	if confirmation.BatchToken != BatchDeleteConfirmation(skills) {
		return Result{}, ErrConfirmationRequired
	}
	result := Result{Skills: append([]inventory.Skill(nil), skills...)}
	for _, skill := range skills {
		if err := removeSkillPath(skill.EncounteredPath); err != nil {
			return result, err
		}
		result.Paths = append(result.Paths, skill.EncounteredPath)
	}
	return result, nil
}

func (m Manager) deleteSingleSelected(skill inventory.Skill, typedName string) (Result, error) {
	if err := DeleteActive(skill, m.Config, typedName); err != nil {
		return Result{}, err
	}
	return Result{Skills: []inventory.Skill{skill}, Paths: []string{skill.EncounteredPath}}, nil
}

func (m Manager) Quarantine(skill inventory.Skill, confirm bool) (string, error) {
	if !m.Config.CanWrite(skill.Root) {
		return "", ErrWritePermissionRequired
	}
	if !confirm {
		return "", ErrConfirmationRequired
	}
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dest := filepath.Join(m.QuarantineDir, timestamp, safeName(skill.Name))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(skill.EncounteredPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (m Manager) QuarantinedSkills() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(m.QuarantineDir, "*", "*"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for i := len(matches) - 1; i >= 0; i-- {
		name := filepath.Base(matches[i])
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, nil
}

func (m Manager) Restore(name string, destRoot string) (string, error) {
	if !m.Config.CanWrite(destRoot) {
		return "", ErrWritePermissionRequired
	}
	matches, err := filepath.Glob(filepath.Join(m.QuarantineDir, "*", safeName(name)))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no quarantined skill named %q", name)
	}
	latest := matches[len(matches)-1]
	dest := filepath.Join(destRoot, filepath.Base(latest))
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("restore destination already exists: %s", dest)
	}
	if err := os.Rename(latest, dest); err != nil {
		return "", err
	}
	return dest, nil
}

type RenamePreview struct {
	OldPath       string
	NewPath       string
	OldName       string
	NewName       string
	Warn          string
	Frontmatter   string
	WouldModifyMD bool
}

func PreviewRename(skill inventory.Skill, newName string) RenamePreview {
	newName = strings.TrimSpace(newName)
	preview := RenamePreview{OldPath: skill.EncounteredPath, OldName: skill.Name, NewName: newName}
	preview.NewPath = filepath.Join(filepath.Dir(skill.EncounteredPath), newName)
	if skill.IsSymlink || strings.Contains(skill.Provenance, "package") {
		preview.Warn = "skill appears symlinked or package-managed; quarantine is safer than rename"
	}
	if skill.PrimaryPath != "" && filepath.Base(skill.PrimaryPath) == "SKILL.md" {
		preview.WouldModifyMD = true
		preview.Frontmatter = fmt.Sprintf("name: %s -> name: %s", skill.Name, newName)
	}
	return preview
}

func Rename(skill inventory.Skill, newName string, cfg config.Config, confirm bool) (RenamePreview, error) {
	preview := PreviewRename(skill, newName)
	if preview.NewName == "" {
		return preview, ErrRenameNameRequired
	}
	if !cfg.CanWrite(skill.Root) {
		return preview, ErrWritePermissionRequired
	}
	if !confirm {
		return preview, ErrConfirmationRequired
	}
	if preview.Warn != "" {
		return preview, errors.New(preview.Warn)
	}
	if _, err := os.Stat(preview.NewPath); err == nil {
		return preview, fmt.Errorf("rename destination already exists: %s", preview.NewPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return preview, err
	}
	if err := os.Rename(skill.EncounteredPath, preview.NewPath); err != nil {
		return preview, err
	}
	if skill.PrimaryPath != "" && preview.WouldModifyMD {
		newPrimaryPath := filepath.Join(preview.NewPath, filepath.Base(skill.PrimaryPath))
		data, err := os.ReadFile(newPrimaryPath)
		if err != nil {
			_ = os.Rename(preview.NewPath, skill.EncounteredPath)
			return preview, err
		}
		updated, changed := updateSkillNameFrontmatter(string(data), skill.Name, preview.NewName)
		if changed {
			if err := os.WriteFile(newPrimaryPath, []byte(updated), 0o644); err != nil {
				_ = os.Rename(preview.NewPath, skill.EncounteredPath)
				return preview, err
			}
		}
	}
	return preview, nil
}

func updateSkillNameFrontmatter(content, oldName, newName string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		lineEnding := strings.TrimPrefix(line, trimmed)
		for _, oldLine := range []string{"name: " + oldName, "name: \"" + oldName + "\"", "name: '" + oldName + "'"} {
			if trimmed == oldLine {
				lines[i] = "name: " + newName + lineEnding
				return strings.Join(lines, ""), true
			}
		}
	}
	return content, false
}

func DeleteActive(skill inventory.Skill, cfg config.Config, typedName string) error {
	if !cfg.CanWrite(skill.Root) {
		return ErrWritePermissionRequired
	}
	if typedName != skill.Name {
		return fmt.Errorf("active skill deletion requires typing %q", skill.Name)
	}
	return removeSkillPath(skill.EncounteredPath)
}

func DeleteQuarantined(path string, confirm bool) error {
	if !confirm {
		return ErrConfirmationRequired
	}
	return removeSkillPath(path)
}

func removeSkillPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
