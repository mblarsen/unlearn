package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLAdapterScansDerivedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"message":"read /tmp/skills/alpha/SKILL.md"}` + "\n" +
		`{"message":"beta was mentioned only"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := JSONLAdapter{}.Scan(path, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	grades := map[string]EvidenceGrade{}
	for _, item := range evidence {
		grades[item.SkillName] = item.Grade
	}
	if grades["alpha"] != EvidenceStrong {
		t.Fatalf("alpha grade=%s", grades["alpha"])
	}
	if grades["beta"] != EvidenceWeak {
		t.Fatalf("beta grade=%s", grades["beta"])
	}
}
