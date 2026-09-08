package tui

import (
	"os"
	"path/filepath"
	"testing"

	fsactions "github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestConfigActionServicePersistsMixedDeleteReconciliation(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowWrite(root)
	skills := []inventory.Skill{
		{ID: "existing", Name: "demo", Root: root, EncounteredPath: existing},
		{ID: "missing", Name: "demo", Root: root, EncounteredPath: filepath.Join(root, "missing")},
	}
	finding := analysis.Finding{ID: "duplicate:demo", Type: analysis.FindingDuplicate, Skills: skills}
	indexPath := filepath.Join(t.TempDir(), "index.db")
	db, err := state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ReplaceIndex(db, skills, []analysis.Finding{finding}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	service := &ConfigActionService{Config: cfg, IndexPath: indexPath}
	result, err := service.DeleteSelected(skills, fsactions.DeleteConfirmation{BatchToken: fsactions.BatchDeleteConfirmation(skills)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 2 || len(result.Missing) != 1 {
		t.Fatalf("result=%#v", result)
	}

	db, err = state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cachedSkills, cachedFindings, err := state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cachedSkills) != 0 || len(cachedFindings) != 0 {
		t.Fatalf("persisted stale inventory: skills=%#v findings=%#v", cachedSkills, cachedFindings)
	}
}

func TestConfigActionServicePersistsCompletedDeleteBeforePartialFailure(t *testing.T) {
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
	skills := []inventory.Skill{
		{ID: "first", Name: "demo", Root: root, EncounteredPath: first},
		{ID: "blocked", Name: "demo", Root: root, EncounteredPath: filepath.Join(blockingFile, "blocked")},
	}
	finding := analysis.Finding{ID: "duplicate:demo", Type: analysis.FindingDuplicate, Skills: skills}
	indexPath := filepath.Join(t.TempDir(), "index.db")
	db, err := state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ReplaceIndex(db, skills, []analysis.Finding{finding}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	service := &ConfigActionService{Config: cfg, IndexPath: indexPath}
	result, err := service.DeleteSelected(skills, fsactions.DeleteConfirmation{BatchToken: fsactions.BatchDeleteConfirmation(skills)})
	if err == nil {
		t.Fatal("expected partial delete failure")
	}
	if len(result.Skills) != 1 || result.Skills[0].ID != "first" {
		t.Fatalf("result=%#v", result)
	}

	db, err = state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cachedSkills, cachedFindings, err := state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cachedSkills) != 1 || cachedSkills[0].ID != "blocked" {
		t.Fatalf("cached skills=%#v", cachedSkills)
	}
	if len(cachedFindings) != 0 {
		t.Fatalf("duplicate finding should be pruned: %#v", cachedFindings)
	}
}
