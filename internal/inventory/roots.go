package inventory

import (
	"os"
	"path/filepath"
	"sort"
)

func KnownGlobalRoots() []string {
	active, inactive := CandidateAgentIDs()
	roots := RootsForAgents(append(active, inactive...))
	if len(roots) > 0 {
		return roots
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	roots = []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".pi", "agent", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(configHome, "agents", "skills"),
		filepath.Join(configHome, "opencode", "skills"),
	}
	sort.Strings(roots)
	return roots
}
