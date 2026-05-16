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

type Manager struct {
	Config        config.Config
	QuarantineDir string
}

func (m Manager) Quarantine(skill inventory.Skill, confirm bool) (string, error) {
	if !m.Config.CanWrite(skill.Root) {
		return "", ErrWritePermissionRequired
	}
	if !confirm {
		return "", errors.New("confirmation required")
	}
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	dest := filepath.Join(m.QuarantineDir, timestamp, safeName(skill.Name))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(skill.EncounteredPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (m Manager) Restore(name string, destRoot string) (string, error) {
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
	if !cfg.CanWrite(skill.Root) {
		return preview, ErrWritePermissionRequired
	}
	if !confirm {
		return preview, errors.New("confirmation required")
	}
	if preview.Warn != "" {
		return preview, errors.New(preview.Warn)
	}
	if skill.PrimaryPath != "" && preview.WouldModifyMD {
		data, err := os.ReadFile(skill.PrimaryPath)
		if err != nil {
			return preview, err
		}
		oldLine := "name: " + skill.Name
		newLine := "name: " + newName
		updated := strings.Replace(string(data), oldLine, newLine, 1)
		if updated == string(data) {
			updated = strings.Replace(string(data), "name: \""+skill.Name+"\"", "name: \""+newName+"\"", 1)
		}
		if err := os.WriteFile(skill.PrimaryPath, []byte(updated), 0o644); err != nil {
			return preview, err
		}
	}
	if err := os.Rename(skill.EncounteredPath, preview.NewPath); err != nil {
		return preview, err
	}
	return preview, nil
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
