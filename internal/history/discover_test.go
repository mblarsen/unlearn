package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPiJSONLIsBoundedAndReadOnly(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".pi", "agent", "sessions", "run")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.jsonl", "a.jsonl", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("private"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := DiscoverPiJSONL(home, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "a.jsonl" {
		t.Fatalf("paths=%v", paths)
	}
}
