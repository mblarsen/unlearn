package actions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestQuarantineRequiresWritePermission(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "demo")
	if err := os.Mkdir(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := Manager{Config: config.Default(), QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}
	_, err := mgr.Quarantine(inventory.Skill{Name: "demo", Root: root, EncounteredPath: skillPath}, true)
	if !errors.Is(err, ErrWritePermissionRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestQuarantineSameNameInstallsDoNotCollide(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	if err := os.Mkdir(one, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(two, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg, QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}
	first, err := mgr.Quarantine(inventory.Skill{Name: "demo", Root: root, EncounteredPath: one}, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Quarantine(inventory.Skill{Name: "demo", Root: root, EncounteredPath: two}, true)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("quarantine destinations collided: %s", first)
	}
}

func TestQuarantineSelectedRequiresConfirmationAndWriteForAllInstalls(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "demo")
	if err := os.Mkdir(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	mgr := Manager{Config: cfg, QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}
	_, err := mgr.QuarantineSelected([]inventory.Skill{{Name: "demo", Root: root, EncounteredPath: skillPath}}, false)
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	_, err = mgr.QuarantineSelected([]inventory.Skill{{Name: "demo", Root: root, EncounteredPath: skillPath}}, true)
	if !errors.Is(err, ErrWritePermissionRequired) {
		t.Fatalf("expected write permission error, got %v", err)
	}
}

func TestQuarantineSelectedMovesEverySelectedInstall(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	if err := os.Mkdir(one, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(two, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg, QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}
	result, err := mgr.QuarantineSelected([]inventory.Skill{
		{Name: "demo", Root: root, EncounteredPath: one},
		{Name: "demo", Root: root, EncounteredPath: two},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Paths) != 2 || result.Paths[0] == result.Paths[1] {
		t.Fatalf("unexpected result paths=%v", result.Paths)
	}
	for _, path := range result.Paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing quarantined path %s: %v", path, err)
		}
	}
}

func TestQuarantineAndRestoreFixture(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "demo")
	if err := os.Mkdir(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg, QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}
	quarantined, err := mgr.Quarantine(inventory.Skill{Name: "demo", Root: root, EncounteredPath: skillPath}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(quarantined); err != nil {
		t.Fatal(err)
	}
	restored, err := mgr.Restore("demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if restored != skillPath {
		t.Fatalf("restored=%s want %s", restored, skillPath)
	}
}

func TestRestoreRequiresWritePermission(t *testing.T) {
	root := t.TempDir()
	quarantineDir := filepath.Join(t.TempDir(), "quarantine")
	quarantined := filepath.Join(quarantineDir, "20260101T000000.000000000Z", "demo")
	if err := os.MkdirAll(quarantined, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := Manager{Config: config.Default(), QuarantineDir: quarantineDir}
	_, err := mgr.Restore("demo", root)
	if !errors.Is(err, ErrWritePermissionRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveSelectionPrefersMarkedThenAllThenCursor(t *testing.T) {
	skills := []inventory.Skill{{Name: "one"}, {Name: "two"}, {Name: "three"}}
	selected, err := ResolveSelection(SelectionInput{Kind: DestructiveDelete, Choices: skills, Cursor: 2, Marked: map[int]bool{1: true}, AllowAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "two" {
		t.Fatalf("selected=%v", selected)
	}
	selected, err = ResolveSelection(SelectionInput{Kind: DestructiveQuarantine, Choices: skills, Cursor: len(skills), AllowAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != len(skills) {
		t.Fatalf("expected all installs, got %v", selected)
	}
	selected, err = ResolveSelection(SelectionInput{Kind: DestructiveRename, Choices: skills, Cursor: 99, AllowAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "three" {
		t.Fatalf("expected clamped cursor selection, got %v", selected)
	}
}

func TestDuplicateRootChoicesOnlyUsesDuplicateFindings(t *testing.T) {
	findings := []analysis.Finding{
		{ID: "duplicate:alpha", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{{Name: "alpha", Root: "/keep"}, {Name: "alpha", Root: "/drop"}}},
		{ID: "conflict:beta", Type: analysis.FindingConflict, Skills: []inventory.Skill{{Name: "beta", Root: "/drop"}, {Name: "beta", Root: "/other"}}},
		{ID: "duplicate:gamma", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{{Name: "gamma", Root: "/keep"}, {Name: "gamma", Root: "/drop"}}},
	}
	choices := DuplicateRootChoices(findings)
	if len(choices) != 2 {
		t.Fatalf("choices=%v", choices)
	}
	if choices[0].Root != "/drop" || len(choices[0].Skills) != 2 {
		t.Fatalf("expected /drop to cover two duplicate findings, got %#v", choices[0])
	}
	for _, skill := range choices[0].Skills {
		if skill.Name == "beta" {
			t.Fatalf("conflict finding leaked into batch duplicate cleanup: %#v", choices[0])
		}
	}
}

func TestRenamePreviewTrimsInputAndRenameRejectsBlankName(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "old")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preview := PreviewRename(inventory.Skill{Name: "old", Root: root, EncounteredPath: skillDir}, "  new  ")
	if preview.NewName != "new" || preview.NewPath != filepath.Join(root, "new") {
		t.Fatalf("preview=%#v", preview)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	_, err := Rename(inventory.Skill{Name: "old", Root: root, EncounteredPath: skillDir}, "   ", cfg, true)
	if !errors.Is(err, ErrRenameNameRequired) {
		t.Fatalf("expected blank rename error, got %v", err)
	}
}

func TestRenamePreviewWarnsForSymlink(t *testing.T) {
	preview := PreviewRename(inventory.Skill{Name: "old", EncounteredPath: "/tmp/root/old", IsSymlink: true, PrimaryPath: "/tmp/root/old/SKILL.md"}, "new")
	if preview.Warn == "" || !preview.WouldModifyMD {
		t.Fatalf("preview=%#v", preview)
	}
}

func TestDeleteActiveRequiresTypedName(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	err := DeleteActive(inventory.Skill{Name: "demo", Root: root, EncounteredPath: skillDir}, cfg, "wrong")
	if err == nil {
		t.Fatal("expected typed-name error")
	}
	if err := DeleteActive(inventory.Skill{Name: "demo", Root: root, EncounteredPath: skillDir}, cfg, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill dir still exists, err=%v", err)
	}
}

func TestDeleteQuarantinedRequiresConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(DeleteQuarantined(path, false), ErrConfirmationRequired) {
		t.Fatal("expected confirmation error")
	}
	if err := DeleteQuarantined(path, true); err != nil {
		t.Fatal(err)
	}
}

func TestRenameRejectsExistingDestinationBeforeFrontmatterChange(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "old")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	original := []byte("---\nname: old\ndescription: demo\n---\nBody")
	if err := os.WriteFile(skillFile, original, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	if _, err := Rename(inventory.Skill{Name: "old", Root: root, EncounteredPath: skillDir, PrimaryPath: skillFile}, "new", cfg, true); err == nil {
		t.Fatal("expected destination exists error")
	}
	data, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("frontmatter changed despite failed rename: %s", data)
	}
}

func TestRenameDoesNotReplacePartialFrontmatterName(t *testing.T) {
	content := "---\nname: old-helper\ndescription: old\n---\nBody"
	updated, changed := updateSkillNameFrontmatter(content, "old", "new")
	if changed || updated != content {
		t.Fatalf("partial name should not change: changed=%v content=%s", changed, updated)
	}
}

func TestRenameUpdatesDirectoryAndFrontmatter(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "old")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: old\ndescription: demo\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	preview, err := Rename(inventory.Skill{Name: "old", Root: root, EncounteredPath: skillDir, PrimaryPath: skillFile}, "new", cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(preview.NewPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(preview.NewPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\nname: new\ndescription: demo\n---\nBody" {
		t.Fatalf("frontmatter not updated: %s", data)
	}
}
