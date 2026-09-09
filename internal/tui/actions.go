package tui

import (
	"context"
	"fmt"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/workbench"
)

type ActionService interface {
	KeepSkill(skill inventory.Skill) error
	IgnoreFinding(finding analysis.Finding) error
	FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool)
	AllowWrite(root string) error
	Mutate(request workbench.Request) workbench.Outcome
	PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview
	QuarantinedSkills() ([]string, error)
	DraftMerge(ctx context.Context, skills []inventory.Skill) (llm.DraftResult, error)
}

type NoopActionService struct{}

func (NoopActionService) KeepSkill(skill inventory.Skill) error        { return nil }
func (NoopActionService) IgnoreFinding(finding analysis.Finding) error { return nil }
func (NoopActionService) FirstMissingWrite(skills []inventory.Skill) (inventory.Skill, bool) {
	return inventory.Skill{}, false
}
func (NoopActionService) AllowWrite(root string) error { return nil }
func (NoopActionService) Mutate(request workbench.Request) workbench.Outcome {
	outcome := workbench.Outcome{Snapshot: request.Snapshot.Clone()}
	switch request.Kind {
	case workbench.Quarantine, workbench.Delete:
		outcome.Removed = append([]inventory.Skill(nil), request.Targets...)
		outcome.Snapshot = inventorysnapshot.Remove(outcome.Snapshot, request.Targets)
	case workbench.Rename:
		if len(request.Targets) == 1 {
			preview := fsactions.PreviewRename(request.Targets[0], request.NewName)
			outcome.RenamePreview = preview
			outcome.Snapshot, _ = inventorysnapshot.Rename(outcome.Snapshot, request.Targets[0], preview.NewName, preview.NewPath)
		}
	}
	return outcome
}
func (NoopActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}
func (NoopActionService) QuarantinedSkills() ([]string, error) { return nil, nil }
func (NoopActionService) DraftMerge(_ context.Context, skills []inventory.Skill) (llm.DraftResult, error) {
	return llm.DraftResult{}, fmt.Errorf("LLM merged-draft generation is not configured for this dashboard")
}

type ConfigActionService struct {
	ConfigPath     string
	Config         config.Config
	IndexPath      string
	QuarantineDir  string
	LLMCacheDir    string
	DraftGenerator llm.DraftGenerator
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

func (s *ConfigActionService) Mutate(request workbench.Request) workbench.Outcome {
	module := workbench.Module{Config: s.Config, IndexPath: s.IndexPath, QuarantineDir: s.QuarantineDir}
	return module.Execute(request)
}

func (s *ConfigActionService) PreviewRename(skill inventory.Skill, newName string) fsactions.RenamePreview {
	return fsactions.PreviewRename(skill, newName)
}

func (s *ConfigActionService) QuarantinedSkills() ([]string, error) {
	mgr := fsactions.Manager{Config: s.Config, QuarantineDir: s.QuarantineDir}
	return mgr.QuarantinedSkills()
}

func NewDraftGeneratorFromEnv(cacheDir string) llm.DraftGenerator {
	analyzer, ok := llm.NewGeminiAnalyzerFromEnv()
	if !ok {
		return unavailableDraftGenerator{}
	}
	return llm.NewCachedDraftGenerator(cacheDir, analyzer)
}

func (s *ConfigActionService) DraftMerge(ctx context.Context, skills []inventory.Skill) (llm.DraftResult, error) {
	if len(skills) < 2 {
		return llm.DraftResult{}, fmt.Errorf("select at least two skills to draft a merge")
	}
	if !s.Config.LLMAssisted {
		return llm.DraftResult{}, fmt.Errorf("LLM-assisted analysis is off; rerun with --with-llm or enable it in setup to generate preview drafts")
	}
	generator := s.DraftGenerator
	if generator == nil {
		generator = unavailableDraftGenerator{}
	}
	return generator.GenerateMergedSkillDraft(ctx, llm.DraftRequest{Skills: draftSkills(skills), PromptVersion: llm.DraftPromptVersion})
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
