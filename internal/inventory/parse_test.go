package inventory

import "testing"

func TestParseSkillMarkdown(t *testing.T) {
	front, body := ParseSkillMarkdown([]byte("---\nname: demo\ndescription: 'Does work'\n---\n# Body\n"))
	if front["name"] != "demo" || front["description"] != "Does work" {
		t.Fatalf("unexpected frontmatter: %#v", front)
	}
	if body != "# Body" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestExtractReferences(t *testing.T) {
	refs := ExtractReferences("Read [guide](references/guide.md), scripts/run.sh and https://example.com/no.md")
	want := map[string]bool{"references/guide.md": true, "scripts/run.sh": true}
	if len(refs) != len(want) {
		t.Fatalf("refs=%v", refs)
	}
	for _, ref := range refs {
		if !want[ref] {
			t.Fatalf("unexpected ref %q in %v", ref, refs)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens([]byte("one two three four")); got != 4 {
		t.Fatalf("tokens=%d", got)
	}
}

func TestActivationRisk(t *testing.T) {
	if got := ActivationRisk("Use before any plan, build, implement, review, fix, debug, optimize task", ""); got != "high" {
		t.Fatalf("risk=%s", got)
	}
	if got := ActivationRisk("Use for a narrow Cloudflare KV migration task", ""); got == "high" {
		t.Fatalf("risk unexpectedly high")
	}
}
