package tui

import (
	"context"
	"fmt"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
)

type ActionService interface {
	KeepSkill(skill inventory.Skill) error
	IgnoreFinding(finding analysis.Finding) error
	FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool)
	AllowWrite(root string) error
	QuarantineSelected(skills []inventory.Skill) (fsactions.Result, error)
	DeleteSelected(skills []inventory.Skill, confirmation fsactions.DeleteConfirmation) (fsactions.Result, error)
	PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview
	Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error)
	QuarantinedSkills() ([]string, error)
	Restore(name string, destRoot string) (string, error)
	DraftMerge(skills []inventory.Skill) (llm.DraftResult, error)
}

type NoopActionService struct{}

func (NoopActionService) KeepSkill(skill inventory.Skill) error        { return nil }
func (NoopActionService) IgnoreFinding(finding analysis.Finding) error { return nil }
func (NoopActionService) FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool) {
	return inventory.Skill{}, false
}
func (NoopActionService) AllowWrite(root string) error { return nil }
func (NoopActionService) QuarantineSelected(skills []inventory.Skill) (fsactions.Result, error) {
	return fsactions.Result{Skills: append([]inventory.Skill(nil), skills...)}, nil
}
func (NoopActionService) DeleteSelected(skills []inventory.Skill, confirmation fsactions.DeleteConfirmation) (fsactions.Result, error) {
	return fsactions.Result{Skills: append([]inventory.Skill(nil), skills...)}, nil
}
func (NoopActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (NoopActionService) Rename(skill inventory.Skill, newName string) (fsactions.RenamePreview, error) {
	return fsactions.PreviewRename(skill, newName), nil
}
func (NoopActionService) QuarantinedSkills() ([]string, error)                 { return nil, nil }
func (NoopActionService) Restore(name string, destRoot string) (string, error) { return "", nil }
func (NoopActionService) DraftMerge(skills []inventory.Skill) (llm.DraftResult, error) {
	return llm.DraftResult{}, fmt.Errorf("LLM merged-draft generation is not configured for this dashboard")
}

type ConfigActionService struct {
	ConfigPath    string
	Config        config.Config
	QuarantineDir string
	LLMCacheDir   string
}

func (s *ConfigActionService) KeepSkill(skill inventory.Skill) error {
	s.Config.KeepSkill(skill.Name)
	return s.save()
}

func (s *ConfigActionService) IgnoreFinding(finding analysis.Finding) error {
	s.Config.IgnoreFinding(finding.ID, "ignored from dashboard")
	return s.save()
}

func (s *ConfigActionService) FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool) {
	return fsactions.FirstMissingWrite(s.Config, skills)
}

func (s *ConfigActionService) AllowWrite(root string) error {
	s.Config.TrustRoot(root)
	s.Config.AllowWrite(root)
	return s.save()
}

func (s *ConfigActionService) QuarantineSelected(skills []inventory.Skill) (fsactions.Result, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.QuarantineSelected(skills, true)
}

func (s *ConfigActionService) DeleteSelected(skills []inventory.Skill, confirmation fsactions.DeleteConfirmation) (fsactions.Result, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.DeleteSelected(skills, confirmation)
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
	if !s.Config.CanWrite(destRoot) {
		return "", fsactions.ErrWritePermissionRequired
	}
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.Restore(name, destRoot)
}

func (s *ConfigActionService) DraftMerge(skills []inventory.Skill) (llm.DraftResult, error) {
	if len(skills) < 2 {
		return llm.DraftResult{}, fmt.Errorf("select at least two skills to draft a merge")
	}
	if !s.Config.LLMAssisted {
		return llm.DraftResult{}, fmt.Errorf("LLM-assisted analysis is off; rerun with --with-llm or enable it in setup to generate preview drafts")
	}
	analyzer, ok := llm.NewGeminiAnalyzerFromEnv()
	if !ok {
		return llm.DraftResult{}, fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY is required for preview draft generation")
	}
	generator := llm.NewCachedDraftGenerator(s.LLMCacheDir, analyzer)
	return generator.GenerateMergedSkillDraft(context.Background(), llm.DraftRequest{Skills: draftSkills(skills), PromptVersion: llm.DraftPromptVersion})
}

func draftSkills(skills []inventory.Skill) []llm.DraftSkill {
	out := make([]llm.DraftSkill, 0, len(skills))
	for _, skill := range skills {
		path := skill.PrimaryPath
		if path == "" {
			path = skill.EncounteredPath
		}
		out = append(out, llm.DraftSkill{
			Name:        skill.Name,
			Description: skill.Description,
			Frontmatter: skill.Frontmatter,
			Body:        skill.Body,
			ContentHash: skill.ContentHash,
			Path:        path,
		})
	}
	return out
}

func (s *ConfigActionService) save() error {
	if s.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	return s.Config.Save(s.ConfigPath)
}
