package inventory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSkillInstallParserParsesInstallInterpretation(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "planner")
	primary := filepath.Join(skillDir, "SKILL.md")
	write(t, primary, "---\nname: planner\ndescription: MUST use before any planning task\n---\nRead references/guide.md and references/missing.md")
	write(t, filepath.Join(skillDir, "references", "guide.md"), "support words")

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	parser := skillInstallParser{now: func() time.Time { return now }}
	skill, err := parser.Parse(skillInstall{
		Root:            root,
		EncounteredPath: skillDir,
		ResolvedPath:    skillDir,
		PrimaryPath:     primary,
		Kind:            KindDirectory,
		Ownership:       RootOwnership{ActiveAgents: []string{"pi"}, InactiveAgents: []string{"codex"}},
		RootKnown:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if skill.Name != "planner" || skill.PrimaryPath != primary || skill.Kind != KindDirectory {
		t.Fatalf("unexpected parsed skill: %#v", skill)
	}
	if skill.ActivationRisk != "high" {
		t.Fatalf("risk=%s signals=%v", skill.ActivationRisk, skill.ActivationRiskSignals)
	}
	if skill.LowerTokens <= 0 || skill.UpperTokens <= skill.LowerTokens {
		t.Fatalf("bad token cost range %d-%d", skill.LowerTokens, skill.UpperTokens)
	}
	if len(skill.SupportRefs) != 2 || !skill.SupportRefs[1].Broken {
		t.Fatalf("support refs=%#v", skill.SupportRefs)
	}
	if len(skill.BrokenRefs) != 1 || skill.BrokenRefs[0] != "references/missing.md" {
		t.Fatalf("broken refs=%v", skill.BrokenRefs)
	}
	if skill.ContentHash == "" || skill.Provenance != "user-provided root" {
		t.Fatalf("hash/provenance = %q / %q", skill.ContentHash, skill.Provenance)
	}
	if !skill.RootKnown || skill.ActiveAgents[0] != "pi" || skill.InactiveAgents[0] != "codex" || !skill.ScannedAt.Equal(now) {
		t.Fatalf("metadata not assigned locally: %#v", skill)
	}
}

func TestSkillInstallParserUnknownAndBrokenInstalls(t *testing.T) {
	now := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)
	parser := skillInstallParser{now: func() time.Time { return now }}
	install := skillInstall{
		Root:            filepath.Join("tmp", "skills"),
		EncounteredPath: filepath.Join("tmp", "skills", "legacy"),
		ResolvedPath:    filepath.Join("tmp", "skills", "legacy"),
		Kind:            KindSkillLike,
	}

	unknown := parser.Unknown(install, "legacy")
	if unknown.Name != "legacy" || unknown.Kind != KindSkillLike || unknown.ActivationRisk != "unknown" || unknown.Broken {
		t.Fatalf("unexpected unknown skill: %#v", unknown)
	}

	broken := parser.Broken(install, "dangling")
	if broken.Name != "dangling" || !broken.Broken || !broken.ReadOnly || broken.ActivationRisk != "unknown" {
		t.Fatalf("unexpected broken skill: %#v", broken)
	}
}
