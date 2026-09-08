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

func TestQuarantineSelectedDoesNotReconcileDestinationENOENT(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "demo")
	if err := os.Mkdir(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{
		Config:        cfg,
		QuarantineDir: filepath.Join(t.TempDir(), "quarantine"),
		RenamePath: func(_, destination string) error {
			return &os.PathError{Op: "rename", Path: destination, Err: os.ErrNotExist}
		},
	}

	result, err := mgr.QuarantineSelected([]inventory.Skill{{Name: "demo", Root: root, EncounteredPath: skillPath}}, true)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination ENOENT must remain visible, got %v", err)
	}
	if len(result.Skills) != 0 || len(result.Missing) != 0 {
		t.Fatalf("existing source was incorrectly reconciled: %#v", result)
	}
	if _, err := os.Lstat(skillPath); err != nil {
		t.Fatalf("source must remain present: %v", err)
	}
}

func TestQuarantineSelectedReconcilesMissingSource(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.AllowWrite(root)
	skill := inventory.Skill{Name: "demo", Root: root, EncounteredPath: filepath.Join(root, "missing")}
	mgr := Manager{Config: cfg, QuarantineDir: filepath.Join(t.TempDir(), "quarantine")}

	result, err := mgr.QuarantineSelected([]inventory.Skill{skill}, true)
	if err != nil {
		t.Fatalf("missing source should reconcile: %v", err)
	}
	if len(result.Skills) != 1 || len(result.Missing) != 1 {
		t.Fatalf("result=%#v", result)
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

func TestDeleteSelectedRequiresTypedNameForSingleInstall(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg}
	_, err := mgr.DeleteSelected([]inventory.Skill{{Name: "demo", Root: root, EncounteredPath: skillDir}}, DeleteConfirmation{TypedName: "wrong"})
	if err == nil {
		t.Fatal("expected typed-name error")
	}
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("single install should not be deleted after wrong typed name: %v", err)
	}
	result, err := mgr.DeleteSelected([]inventory.Skill{{Name: "demo", Root: root, EncounteredPath: skillDir}}, DeleteConfirmation{TypedName: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 1 || result.Paths[0] != skillDir {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill dir still exists, err=%v", err)
	}
}

func TestDeleteSelectedChecksEveryWritePermissionBeforeBatchMutation(t *testing.T) {
	writableRoot := t.TempDir()
	blockedRoot := t.TempDir()
	first := filepath.Join(writableRoot, "first")
	second := filepath.Join(blockedRoot, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.AllowWrite(writableRoot)
	mgr := Manager{Config: cfg}
	skills := []inventory.Skill{
		{Name: "first", Root: writableRoot, EncounteredPath: first},
		{Name: "second", Root: blockedRoot, EncounteredPath: second},
	}

	result, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: BatchDeleteConfirmation(skills)})
	if !errors.Is(err, ErrWritePermissionRequired) {
		t.Fatalf("expected write permission error, got %v", err)
	}
	if len(result.Skills) != 0 {
		t.Fatalf("result=%#v", result)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("permission failure mutated %s: %v", path, err)
		}
	}
}

func TestDeleteSelectedRequiresBatchConfirmationForMultipleInstalls(t *testing.T) {
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
	mgr := Manager{Config: cfg}
	skills := []inventory.Skill{{Name: "one", Root: root, EncounteredPath: one}, {Name: "two", Root: root, EncounteredPath: two}}
	_, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: "wrong"})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if _, err := os.Stat(one); err != nil {
		t.Fatalf("first install should not be deleted after wrong token: %v", err)
	}
	result, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: BatchDeleteConfirmation(skills)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 2 || len(result.Paths) != 2 {
		t.Fatalf("result=%#v", result)
	}
	for _, path := range []string{one, two} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("install still exists at %s, err=%v", path, err)
		}
	}
}

func TestDeleteSelectedReconcilesAllMissingInstalls(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg}
	skills := []inventory.Skill{
		{Name: "one", Root: root, EncounteredPath: filepath.Join(root, "one")},
		{Name: "two", Root: root, EncounteredPath: filepath.Join(root, "two")},
	}

	result, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: BatchDeleteConfirmation(skills)})
	if err != nil {
		t.Fatalf("missing installs should reconcile without failing: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Fatalf("reconciled skills=%d want 2", len(result.Skills))
	}
}

func TestDeleteSelectedHandlesMixedExistingAndMissingInstalls(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg}
	skills := []inventory.Skill{
		{Name: "existing", Root: root, EncounteredPath: existing},
		{Name: "missing", Root: root, EncounteredPath: filepath.Join(root, "missing")},
	}

	result, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: BatchDeleteConfirmation(skills)})
	if err != nil {
		t.Fatalf("mixed delete should reconcile missing install: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Fatalf("reconciled skills=%d want 2", len(result.Skills))
	}
	if _, err := os.Lstat(existing); !os.IsNotExist(err) {
		t.Fatalf("existing install was not deleted: %v", err)
	}
}

func TestDeleteSelectedReportsOnlyCompletedSkillsOnPartialFailure(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	blockingFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	mgr := Manager{Config: cfg}
	skills := []inventory.Skill{
		{Name: "first", Root: root, EncounteredPath: first},
		{Name: "blocked", Root: root, EncounteredPath: filepath.Join(blockingFile, "blocked")},
	}

	result, err := mgr.DeleteSelected(skills, DeleteConfirmation{BatchToken: BatchDeleteConfirmation(skills)})
	if err == nil {
		t.Fatal("expected second install to fail")
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "first" {
		t.Fatalf("completed skills=%v want first only", result.Skills)
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("first install should remain deleted: %v", err)
	}
	if _, err := os.Stat(blockingFile); err != nil {
		t.Fatalf("failure must not remove unrelated file: %v", err)
	}
}

func TestDeleteSelectedRemovesSymlinkWithoutDeletingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	skill := inventory.Skill{Name: "linked", Root: root, EncounteredPath: link, IsSymlink: true, ResolvedPath: target}
	mgr := Manager{Config: cfg}

	if _, err := mgr.DeleteSelected([]inventory.Skill{skill}, DeleteConfirmation{TypedName: skill.Name}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("symlink still exists: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was affected: %v", err)
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

func TestRenameUsesTrimmedNameForDirectoryAndFrontmatter(t *testing.T) {
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
	preview, err := Rename(inventory.Skill{Name: "old", Root: root, EncounteredPath: skillDir, PrimaryPath: skillFile}, "  new  ", cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.NewPath != filepath.Join(root, "new") {
		t.Fatalf("new path=%s", preview.NewPath)
	}
	data, err := os.ReadFile(filepath.Join(preview.NewPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\nname: new\ndescription: demo\n---\nBody" {
		t.Fatalf("frontmatter not trimmed: %s", data)
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
