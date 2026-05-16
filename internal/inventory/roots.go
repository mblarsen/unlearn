package inventory

import (
	"os"
	"path/filepath"
)

func KnownGlobalRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".pi", "agent", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".config", "opencode", "skills"),
	}
}
