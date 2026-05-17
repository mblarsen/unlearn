package config

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

type Config struct {
	SetupComplete bool     `toml:"setup_complete"`
	LLMAssisted   bool     `toml:"llm_assisted"`
	HistoryScan   bool     `toml:"history_scan"`
	HistoryJSONL  []string `toml:"history_jsonl"`

	Roots          map[string]RootTrust `toml:"roots"`
	WriteRoots     map[string]bool      `toml:"write_roots"`
	Keep           DecisionList         `toml:"keep"`
	IgnoreFindings map[string]string    `toml:"ignore_findings"`
	DropCandidates DecisionList         `toml:"drop_candidates"`
}

type RootTrust struct {
	Trusted bool `toml:"trusted"`
}

type DecisionList struct {
	Skills []string `toml:"skills"`
}

func Default() Config {
	return Config{
		Roots:          map[string]RootTrust{},
		WriteRoots:     map[string]bool{},
		IgnoreFindings: map[string]string{},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Roots == nil {
		cfg.Roots = map[string]RootTrust{}
	}
	if cfg.WriteRoots == nil {
		cfg.WriteRoots = map[string]bool{}
	}
	if cfg.IgnoreFindings == nil {
		cfg.IgnoreFindings = map[string]string{}
	}
	return cfg, nil
}

func (c Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".unlearn-config-*.toml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	enc := toml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) IsTrusted(root string) bool {
	trust, ok := c.Roots[filepath.Clean(root)]
	return ok && trust.Trusted
}

func (c *Config) TrustRoot(root string) {
	if c.Roots == nil {
		c.Roots = map[string]RootTrust{}
	}
	c.Roots[filepath.Clean(root)] = RootTrust{Trusted: true}
}

func (c Config) CanWrite(root string) bool {
	return c.WriteRoots[filepath.Clean(root)]
}

func (c *Config) AllowWrite(root string) {
	if c.WriteRoots == nil {
		c.WriteRoots = map[string]bool{}
	}
	c.WriteRoots[filepath.Clean(root)] = true
}

func (c *Config) KeepSkill(name string) {
	c.Keep.Skills = appendUnique(c.Keep.Skills, name)
}

func (c *Config) MarkDropCandidate(name string) {
	c.DropCandidates.Skills = appendUnique(c.DropCandidates.Skills, name)
}

func (c *Config) IgnoreFinding(id, reason string) {
	if c.IgnoreFindings == nil {
		c.IgnoreFindings = map[string]string{}
	}
	c.IgnoreFindings[id] = reason
}

func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func (c Config) TrustedRoots(roots []string) []string {
	trusted := make([]string, 0, len(roots))
	for _, root := range roots {
		if c.IsTrusted(root) {
			trusted = append(trusted, filepath.Clean(root))
		}
	}
	sort.Strings(trusted)
	return trusted
}
