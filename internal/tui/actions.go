package tui

import (
	"fmt"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

type ActionService interface {
	KeepSkill(skill inventory.Skill) error
	IgnoreFinding(finding analysis.Finding) error
	CanWrite(root string) bool
	AllowWrite(root string) error
	Quarantine(skill inventory.Skill) (string, error)
	Delete(skill inventory.Skill, typedName string) error
	PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview
	Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error)
	QuarantinedSkills() ([]string, error)
	Restore(name string, destRoot string) (string, error)
}

type NoopActionService struct{}

func (NoopActionService) KeepSkill(skill inventory.Skill) error                { return nil }
func (NoopActionService) IgnoreFinding(finding analysis.Finding) error         { return nil }
func (NoopActionService) CanWrite(root string) bool                            { return false }
func (NoopActionService) AllowWrite(root string) error                         { return nil }
func (NoopActionService) Quarantine(skill inventory.Skill) (string, error)     { return "", nil }
func (NoopActionService) Delete(skill inventory.Skill, typedName string) error { return nil }
func (NoopActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (NoopActionService) Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error) {
	return fsactions.PreviewRename(skill, newName), nil
}
func (NoopActionService) QuarantinedSkills() ([]string, error)                 { return nil, nil }
func (NoopActionService) Restore(name string, destRoot string) (string, error) { return "", nil }

type ConfigActionService struct {
	ConfigPath    string
	Config        config.Config
	QuarantineDir string
}

func (s *ConfigActionService) KeepSkill(skill inventory.Skill) error {
	s.Config.KeepSkill(skill.Name)
	return s.save()
}

func (s *ConfigActionService) IgnoreFinding(finding analysis.Finding) error {
	s.Config.IgnoreFinding(finding.ID, "ignored from dashboard")
	return s.save()
}

func (s *ConfigActionService) CanWrite(root string) bool {
	return s.Config.CanWrite(root)
}

func (s *ConfigActionService) AllowWrite(root string) error {
	s.Config.TrustRoot(root)
	s.Config.AllowWrite(root)
	return s.save()
}

func (s *ConfigActionService) Quarantine(skill inventory.Skill) (string, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.Quarantine(skill, true)
}

func (s *ConfigActionService) Delete(skill inventory.Skill, typedName string) error {
	return fsactions.DeleteActive(skill, s.Config, typedName)
}

func (s *ConfigActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}

func (s *ConfigActionService) Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error) {
	return fsactions.Rename(skill, newName, s.Config, true)
}

func (s *ConfigActionService) QuarantinedSkills() ([]string, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.QuarantinedSkills()
}

func (s *ConfigActionService) Restore(name string, destRoot string) (string, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.Restore(name, destRoot)
}

func (s *ConfigActionService) save() error {
	if s.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	return s.Config.Save(s.ConfigPath)
}
