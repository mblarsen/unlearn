package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScannerFindsDirectoryMarkdownAndBrokenReferences(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\nRead references/guide.md and references/missing.md")
	write(t, filepath.Join(root, "alpha", "references", "guide.md"), "support words")
	write(t, filepath.Join(root, "standalone.md"), "---\nname: standalone\ndescription: Standalone skill\n---\nBody")

	report, err := NewScanner().Scan(ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skills) != 2 {
		t.Fatalf("skills=%d %#v", len(report.Skills), report.Skills)
	}
	alpha := findSkill(report.Skills, "alpha")
	if alpha == nil {
		t.Fatal("alpha missing")
	}
	if alpha.LowerTokens <= 0 || alpha.UpperTokens <= alpha.LowerTokens {
		t.Fatalf("bad token range %d-%d", alpha.LowerTokens, alpha.UpperTokens)
	}
	if len(alpha.BrokenRefs) != 1 || alpha.BrokenRefs[0] != "references/missing.md" {
		t.Fatalf("broken refs=%v", alpha.BrokenRefs)
	}
}

func TestScannerRecordsBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "broken")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	report, err := NewScanner().Scan(ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skills) != 1 || !report.Skills[0].Broken || !report.Skills[0].ReadOnly {
		t.Fatalf("unexpected skills: %#v", report.Skills)
	}
}

func TestInferProvenance(t *testing.T) {
	got := inferProvenance(filepath.Join("home", ".pi", "agent", "skills"), filepath.Join("home", ".pi", "agent", "skills", "x"), "", false)
	if got != "pi global skills root" {
		t.Fatalf("provenance=%q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findSkill(skills []Skill, name string) *Skill {
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}
