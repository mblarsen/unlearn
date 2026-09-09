package collections

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestExecuteCRUDAndExactMembership(t *testing.T) {
	root := t.TempDir()
	alpha := skill("alpha", filepath.Join(root, "pi", "alpha"), "Build accessible web interfaces", "hash-a", []string{"pi"})
	copy := skill("alpha", filepath.Join(root, "codex", "alpha"), "Build accessible web interfaces", "hash-b", []string{"codex"})
	items := []Collection(nil)

	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: Create, Name: "Frontend"}).Collections
	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: AddMember, Name: "Frontend", InstallPath: alpha.EncounteredPath}).Collections
	if got := items[0].Members; len(got) != 1 || got[0].SkillName != "alpha" || got[0].InstallPath != filepath.Clean(alpha.EncounteredPath) {
		t.Fatalf("members=%#v", got)
	}
	// A second copy with the same skill name is a distinct exact membership.
	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: AddMember, Name: "Frontend", InstallPath: copy.EncounteredPath}).Collections
	if len(items[0].Members) != 2 {
		t.Fatalf("members=%#v", items[0].Members)
	}
	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: RemoveMember, Name: "Frontend", InstallPath: alpha.EncounteredPath}).Collections
	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: Rename, Name: "Frontend", NewName: "UI"}).Collections
	if items[0].Name != "UI" || len(items[0].Members) != 1 {
		t.Fatalf("collection=%#v", items[0])
	}
	items = mustExecute(t, items, []inventory.Skill{alpha, copy}, Command{Kind: Delete, Name: "UI"}).Collections
	if len(items) != 0 {
		t.Fatalf("collections=%#v", items)
	}
}

func TestPreviewKeepsStaleMemberWithoutRetargetingCopy(t *testing.T) {
	oldPath := filepath.Join(t.TempDir(), "gone", "research")
	other := skill("research", filepath.Join(t.TempDir(), "other", "research"), "Search primary sources", "new", []string{"pi"})
	items := []Collection{{Name: "Research", Members: []Member{{SkillName: "research", InstallPath: oldPath}}}}

	result := Execute(items, []inventory.Skill{other}, Command{Kind: Preview, Name: "Research"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	member := result.Preview.Members[0]
	if member.Present || member.Member.InstallPath != filepath.Clean(oldPath) {
		t.Fatalf("stale member retargeted: %#v", member)
	}
	if len(member.OtherCopies) != 1 || member.OtherCopies[0].InstallPath != filepath.Clean(other.EncounteredPath) {
		t.Fatalf("other copies=%#v", member.OtherCopies)
	}
	if member.OtherCopies[0].Comparison != ContentUnknown {
		t.Fatalf("stale comparison=%q", member.OtherCopies[0].Comparison)
	}
}

func TestPreviewReportsInventoryAgentEvidenceAndDivergentCopies(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "pi", "alpha")
	pathB := filepath.Join(t.TempDir(), "codex", "alpha")
	a := skill("alpha", pathA, "UI helper", "one", []string{"pi"})
	a.InactiveAgents = []string{"claude-code"}
	b := skill("alpha", pathB, "Different UI helper", "two", []string{"codex"})
	items := []Collection{{Name: "Frontend", Members: []Member{{SkillName: "alpha", InstallPath: pathA}}}}

	result := Execute(items, []inventory.Skill{b, a}, Command{Kind: Preview, Name: "Frontend"})
	member := result.Preview.Members[0]
	if !member.Present || !reflect.DeepEqual(member.ActiveAgents, []string{"pi"}) || !reflect.DeepEqual(member.InactiveAgents, []string{"claude-code"}) {
		t.Fatalf("availability=%#v", member)
	}
	if len(member.OtherCopies) != 1 || member.OtherCopies[0].Comparison != ContentDivergent {
		t.Fatalf("copies=%#v", member.OtherCopies)
	}
}

func TestContentComparisonRequiresBothObservedHashes(t *testing.T) {
	tests := []struct {
		name      string
		present   bool
		reference string
		copy      string
		want      ContentComparison
	}{
		{name: "missing reference", present: false, reference: "one", copy: "one", want: ContentUnknown},
		{name: "unknown reference hash", present: true, copy: "one", want: ContentUnknown},
		{name: "unknown copy hash", present: true, reference: "one", want: ContentUnknown},
		{name: "equal", present: true, reference: "one", copy: "one", want: ContentEqual},
		{name: "divergent", present: true, reference: "one", copy: "two", want: ContentDivergent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareContent(tt.present, tt.reference, tt.copy); got != tt.want {
				t.Fatalf("comparison=%q want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestionsAreDeterministicExplainedAndManual(t *testing.T) {
	root := t.TempDir()
	sk := []inventory.Skill{
		skill("browser", filepath.Join(root, "browser"), "Automate browser accessibility tests", "b", []string{"pi"}),
		skill("research", filepath.Join(root, "research"), "Research primary sources", "r", []string{"pi"}),
		skill("frontend", filepath.Join(root, "frontend"), "Build accessible frontend interfaces", "f", []string{"codex"}),
	}
	items := []Collection{{Name: "Project"}}
	result := Execute(items, sk, Command{Kind: Suggest, Name: "Project", Query: "accessible frontend browser"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if len(result.Suggestions) != 2 || result.Suggestions[0].SkillName != "frontend" || result.Suggestions[1].SkillName != "browser" {
		t.Fatalf("suggestions=%#v", result.Suggestions)
	}
	if result.Suggestions[0].Reason == "" || len(result.Suggestions[0].Installs) != 1 || result.Suggestions[0].Weak || !result.Suggestions[1].Weak {
		t.Fatalf("missing explanation/install choices: %#v", result.Suggestions[0])
	}
	if len(result.Collections[0].Members) != 0 {
		t.Fatalf("suggestions changed membership: %#v", result.Collections)
	}
	shuffled := []inventory.Skill{sk[2], sk[0], sk[1]}
	again := Execute(items, shuffled, Command{Kind: Suggest, Name: "Project", Query: "accessible frontend browser"})
	if !reflect.DeepEqual(result.Suggestions, again.Suggestions) {
		t.Fatalf("suggestions depend on inventory order:\nfirst=%#v\nagain=%#v", result.Suggestions, again.Suggestions)
	}
	weak := Execute(items, sk, Command{Kind: Suggest, Name: "Project", Query: "primary"})
	if len(weak.Suggestions) != 1 || !weak.Suggestions[0].Weak {
		t.Fatalf("weak suggestion was not labeled: %#v", weak.Suggestions)
	}
}

func TestNamesSupportUnicodeAndRejectCaseFoldDuplicates(t *testing.T) {
	items := mustExecute(t, nil, nil, Command{Kind: Create, Name: "Forschung 🌱"}).Collections
	if items[0].Name != "Forschung 🌱" {
		t.Fatalf("name=%q", items[0].Name)
	}
	result := Execute(items, nil, Command{Kind: Create, Name: "forschung 🌱"})
	if result.Err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func mustExecute(t *testing.T, items []Collection, skills []inventory.Skill, command Command) Result {
	t.Helper()
	result := Execute(items, skills, command)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	return result
}

func skill(name, path, description, hash string, active []string) inventory.Skill {
	return inventory.Skill{Name: name, EncounteredPath: path, ResolvedPath: path, Description: description, ContentHash: hash, ActiveAgents: active}
}
