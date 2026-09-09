package discovery_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/discovery"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestSearchRanksInstalledSkillsWithObservedReasonsAndAccess(t *testing.T) {
	skills := []inventory.Skill{
		{Name: "pull-request-review", Description: "Review pull requests for correctness and security", Root: "/skills/pi", EncounteredPath: "/skills/pi/pull-review", ActiveAgents: []string{"pi", "codex"}, RootKnown: true, HistoryEvidence: "strong"},
		{Name: "github-flow", Description: "Create branches and requests", Root: "/skills/shared", EncounteredPath: "/skills/shared/github-flow", InactiveAgents: []string{"claude-code"}, RootKnown: true},
		{Name: "notes", Description: "Create local notes", Root: "/skills/pi", EncounteredPath: "/skills/pi/notes"},
		{Name: "pull-request-review", Description: "Review pull requests for correctness", Root: "/skills/codex", EncounteredPath: "/skills/codex/pull-review", ActiveAgents: []string{"codex"}, RootKnown: true},
	}
	findings := []analysis.Finding{{ID: "overlap:review:flow", Type: analysis.FindingOverlap, Skills: []inventory.Skill{skills[0], skills[1]}}}

	result := discovery.Search("review pull requests", skills, findings)
	if result.WeakQuery || len(result.Matches) != 2 {
		t.Fatalf("result=%#v", result)
	}
	if got := []string{result.Matches[0].Name, result.Matches[1].Name}; !reflect.DeepEqual(got, []string{"pull-request-review", "github-flow"}) {
		t.Fatalf("ranked names=%v", got)
	}
	first := result.Matches[0]
	if len(first.Installs) != 2 || first.Installs[0].Path != "/skills/codex/pull-review" || first.Installs[1].Path != "/skills/pi/pull-review" {
		t.Fatalf("installs=%#v", first.Installs)
	}
	joined := strings.Join(first.Reasons, "\n")
	for _, want := range []string{"name matches", "description matches"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reasons %q do not contain %q", joined, want)
		}
	}
	if first.Invocation != "observed: strong derived evidence" {
		t.Fatalf("invocation=%q", first.Invocation)
	}
	if first.Weak {
		t.Fatal("top result should match more than one task term")
	}
	if !result.Matches[1].Weak {
		t.Fatal("one-term result should be labeled weak")
	}
	if !reflect.DeepEqual(first.OverlapWith, []string{"github-flow"}) {
		t.Fatalf("overlap=%v", first.OverlapWith)
	}
	if !reflect.DeepEqual(first.Installs[0].ActiveAgents, []string{"codex"}) || !reflect.DeepEqual(first.Installs[1].ActiveAgents, []string{"codex", "pi"}) {
		t.Fatalf("active agents not deterministic: %#v", first.Installs)
	}
}

func TestSearchExplainsWeakAndEmptyMatchesWithoutClaimingAComprehensiveGap(t *testing.T) {
	skills := []inventory.Skill{{Name: "résumé-check", Description: "Check résumé wording"}}

	weak := discovery.Search("help me with a task", skills, nil)
	if !weak.WeakQuery || len(weak.Matches) != 0 || !strings.Contains(weak.Message, "specific terms") {
		t.Fatalf("weak result=%#v", weak)
	}

	empty := discovery.Search("deploy kubernetes", skills, nil)
	if empty.WeakQuery || len(empty.Matches) != 0 || empty.Message != "No observed matching installed skill for this task." {
		t.Fatalf("empty result=%#v", empty)
	}

	unicode := discovery.Search("improve résumé wording", skills, nil)
	if len(unicode.Matches) != 1 || unicode.Matches[0].Name != "résumé-check" {
		t.Fatalf("unicode result=%#v", unicode)
	}
}

func TestSearchOrderDoesNotDependOnInventoryOrder(t *testing.T) {
	first := inventory.Skill{Name: "alpha-browser", Description: "Review browser output", EncounteredPath: "/z/alpha"}
	second := inventory.Skill{Name: "beta-browser", Description: "Review browser output", EncounteredPath: "/a/beta"}
	forward := discovery.Search("review browser", []inventory.Skill{first, second}, nil)
	reverse := discovery.Search("review browser", []inventory.Skill{second, first}, nil)
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("results depend on inventory order:\nforward=%#v\nreverse=%#v", forward, reverse)
	}
}

func TestSearchDoesNotPresentDescriptionTermsAsInvocationEvidence(t *testing.T) {
	result := discovery.Search("browser automation", []inventory.Skill{{Name: "browser", Description: "Browser automation"}}, nil)
	if len(result.Matches) != 1 {
		t.Fatalf("result=%#v", result)
	}
	if result.Matches[0].Invocation != "not observed" {
		t.Fatalf("invocation=%q", result.Matches[0].Invocation)
	}
}
