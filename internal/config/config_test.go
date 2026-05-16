package config

import (
	"path/filepath"
	"testing"
)

func TestConfigTrustAndWriteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.TrustRoot("/tmp/skills")
	cfg.AllowWrite("/tmp/skills")
	cfg.Keep.Skills = []string{"keep-me"}
	cfg.IgnoreFindings = map[string]string{"overlap:a:b": "known"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.IsTrusted("/tmp/skills") || !loaded.CanWrite("/tmp/skills") {
		t.Fatalf("trust/write did not round-trip: %#v", loaded)
	}
	if len(loaded.Keep.Skills) != 1 || loaded.Keep.Skills[0] != "keep-me" {
		t.Fatalf("keep decisions did not round-trip: %#v", loaded.Keep)
	}
	if loaded.IgnoreFindings["overlap:a:b"] != "known" {
		t.Fatalf("ignore decisions did not round-trip: %#v", loaded.IgnoreFindings)
	}
}
