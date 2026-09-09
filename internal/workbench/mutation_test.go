package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/inventorysnapshot"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestExecuteDeleteUsesOneExactInstallIdentityAndPersistsPartialSuccess(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	blockingFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(blockingFile, "blocked")
	cached := []inventory.Skill{
		{ID: "cached-first", Name: "demo", Root: root, EncounteredPath: first, PrimaryPath: filepath.Join(first, "SKILL.md")},
		{ID: "blocked", Name: "demo", Root: root, EncounteredPath: blocked},
	}
	finding := analysis.Finding{ID: "duplicate:demo", Type: analysis.FindingDuplicate, Skills: cached}
	indexPath := seedIndex(t, cached, []analysis.Finding{finding})
	cfg := config.Default()
	cfg.AllowWrite(root)
	module := Module{Config: cfg, IndexPath: indexPath}

	// The action target came from another representation with a different ID.
	targets := []inventory.Skill{
		{ID: "ui-first", Name: "DEMO", Root: root, EncounteredPath: first, PrimaryPath: filepath.Join(first, "SKILL.md")},
		cached[1],
	}
	outcome := module.Execute(Request{
		Kind:         Delete,
		Authorized:   true,
		Snapshot:     inventorysnapshot.Snapshot{Skills: cached, Findings: []analysis.Finding{finding}},
		Targets:      targets,
		Confirmation: fsactions.DeleteConfirmation{BatchToken: fsactions.BatchDeleteConfirmation(targets)},
	})

	if !outcome.HasFailure(FilesystemPhase) || outcome.HasFailure(PersistencePhase) {
		t.Fatalf("failures=%#v", outcome.Failures)
	}
	if len(outcome.Removed) != 1 || len(outcome.Snapshot.Skills) != 1 || outcome.Snapshot.Skills[0].ID != "blocked" {
		t.Fatalf("outcome=%#v", outcome)
	}
	persisted := loadSnapshot(t, indexPath)
	if len(persisted.Skills) != 1 || persisted.Skills[0].ID != "blocked" || len(persisted.Findings) != 0 {
		t.Fatalf("persisted=%#v", persisted)
	}
}

func TestExecuteReportsPersistenceFailureAfterFilesystemChangedWithoutRollback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "demo")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := inventory.Skill{ID: "demo", Name: "demo", Root: root, EncounteredPath: path}
	cfg := config.Default()
	cfg.AllowWrite(root)
	module := Module{Config: cfg, persist: func(inventorysnapshot.Snapshot) error { return errors.New("disk full") }}

	outcome := module.Execute(Request{
		Kind:         Delete,
		Authorized:   true,
		Snapshot:     inventorysnapshot.Snapshot{Skills: []inventory.Skill{skill}},
		Targets:      []inventory.Skill{skill},
		Confirmation: fsactions.DeleteConfirmation{TypedName: "demo"},
	})

	if !outcome.HasFailure(PersistencePhase) || outcome.HasFailure(FilesystemPhase) {
		t.Fatalf("failures=%#v", outcome.Failures)
	}
	if !outcome.RecoveryRequired || len(outcome.Snapshot.Skills) != 0 {
		t.Fatalf("outcome=%#v", outcome)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("filesystem mutation was rolled back or incomplete: %v", err)
	}
}

func TestExecuteRenameReconcilesIdentitySnapshotAndRestart(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old")
	if err := os.Mkdir(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(oldPath, "SKILL.md")
	if err := os.WriteFile(primary, []byte("---\nname: old\ndescription: fixture\n---\n\n# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := inventory.Skill{ID: "old-id", Name: "old", Root: root, EncounteredPath: oldPath, ResolvedPath: oldPath, PrimaryPath: primary, Kind: inventory.KindDirectory, Frontmatter: map[string]string{"name": "old"}}
	finding := analysis.Finding{ID: "duplicate:old", Type: analysis.FindingDuplicate, Skills: []inventory.Skill{old, {ID: "other", Name: "old", Root: "/other", EncounteredPath: "/other/old"}}}
	indexPath := seedIndex(t, []inventory.Skill{old}, []analysis.Finding{finding})
	cfg := config.Default()
	cfg.AllowWrite(root)

	outcome := (Module{Config: cfg, IndexPath: indexPath}).Execute(Request{Kind: Rename, Authorized: true, Snapshot: inventorysnapshot.Snapshot{Skills: []inventory.Skill{old}, Findings: []analysis.Finding{finding}}, Targets: []inventory.Skill{old}, NewName: "new"})

	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	if len(outcome.Snapshot.Skills) != 1 {
		t.Fatalf("snapshot=%#v", outcome.Snapshot)
	}
	renamed := outcome.Snapshot.Skills[0]
	if renamed.Name != "new" || renamed.ID != "" || renamed.EncounteredPath != filepath.Join(root, "new") || renamed.PrimaryPath != filepath.Join(root, "new", "SKILL.md") {
		t.Fatalf("renamed=%#v", renamed)
	}
	if len(outcome.Snapshot.Findings) != 0 {
		t.Fatalf("rename retained stale finding: %#v", outcome.Snapshot.Findings)
	}
	data, err := os.ReadFile(renamed.PrimaryPath)
	if err != nil || !strings.Contains(string(data), "name: new") {
		t.Fatalf("frontmatter=%q err=%v", data, err)
	}
	persisted := loadSnapshot(t, indexPath)
	if len(persisted.Skills) != 1 || persisted.Skills[0].EncounteredPath != renamed.EncounteredPath {
		t.Fatalf("persisted=%#v", persisted)
	}
}

func TestExecuteRestoreScansAndPersistsRestoredInstall(t *testing.T) {
	root := t.TempDir()
	quarantine := t.TempDir()
	stored := filepath.Join(quarantine, "20260518T120000.000000000Z", "demo")
	if err := os.MkdirAll(stored, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stored, "SKILL.md"), []byte("---\nname: demo\ndescription: restored fixture\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := seedIndex(t, nil, nil)
	cfg := config.Default()
	cfg.AllowWrite(root)

	outcome := (Module{Config: cfg, IndexPath: indexPath, QuarantineDir: quarantine}).Execute(Request{Kind: Restore, Authorized: true, Snapshot: inventorysnapshot.Snapshot{}, RestoreName: "demo", DestinationRoot: root})

	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	if outcome.Restored == nil || outcome.Restored.Name != "demo" || outcome.Restored.EncounteredPath != filepath.Join(root, "demo") {
		t.Fatalf("outcome=%#v", outcome)
	}
	persisted := loadSnapshot(t, indexPath)
	if len(persisted.Skills) != 1 || persisted.Skills[0].EncounteredPath != filepath.Join(root, "demo") {
		t.Fatalf("persisted=%#v", persisted)
	}
}

func TestExecuteDeleteRemovesSymlinkWithoutFollowingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "demo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	skill := inventory.Skill{ID: "demo", Name: "demo", Root: root, EncounteredPath: link, ResolvedPath: target, IsSymlink: true}
	cfg := config.Default()
	cfg.AllowWrite(root)

	outcome := (Module{Config: cfg}).Execute(Request{Kind: Delete, Authorized: true, Snapshot: inventorysnapshot.Snapshot{Skills: []inventory.Skill{skill}}, Targets: []inventory.Skill{skill}, Confirmation: fsactions.DeleteConfirmation{TypedName: "demo"}})
	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target changed: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink remains: %v", err)
	}
}

func seedIndex(t *testing.T, skills []inventory.Skill, findings []analysis.Finding) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	db, err := state.OpenIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := state.ReplaceIndex(db, skills, findings); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadSnapshot(t *testing.T, path string) inventorysnapshot.Snapshot {
	t.Helper()
	db, err := state.OpenIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	skills, findings, err := state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	return inventorysnapshot.Snapshot{Skills: skills, Findings: findings}
}
