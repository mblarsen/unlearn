package tui

import (
	"path/filepath"
	"testing"

	"github.com/mblarsen/unlearn/internal/collections"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestConfigActionServicePersistsCollectionsWithoutChangingOtherConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.SetupComplete = true
	cfg.LLMAssisted = true
	cfg.KeepSkill("protected")
	service := &ConfigActionService{ConfigPath: path, Config: cfg}
	skill := inventory.Skill{Name: "alpha", EncounteredPath: filepath.Join(t.TempDir(), "skills", "alpha")}

	result := service.ExecuteCollection(collections.Command{Kind: collections.Create, Name: "Frontend"}, []inventory.Skill{skill})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	result = service.ExecuteCollection(collections.Command{Kind: collections.AddMember, Name: "Frontend", InstallPath: skill.EncounteredPath}, []inventory.Skill{skill})
	if result.Err != nil {
		t.Fatal(result.Err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SetupComplete || !loaded.LLMAssisted || len(loaded.Keep.Skills) != 1 || loaded.Keep.Skills[0] != "protected" {
		t.Fatalf("unrelated config changed: %#v", loaded)
	}
	if len(loaded.Collections) != 1 || len(loaded.Collections[0].Members) != 1 {
		t.Fatalf("collections=%#v", loaded.Collections)
	}
}

func TestConfigActionServiceRestoresMemoryOnSaveFailure(t *testing.T) {
	service := &ConfigActionService{ConfigPath: "", Config: config.Default()}
	result := service.ExecuteCollection(collections.Command{Kind: collections.Create, Name: "Frontend"}, nil)
	if result.Err == nil || len(service.Config.Collections) != 0 {
		t.Fatalf("result=%#v config=%#v", result, service.Config.Collections)
	}
}
