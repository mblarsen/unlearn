package actions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
