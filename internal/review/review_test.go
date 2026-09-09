package review

import (
	"path/filepath"
	"testing"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/inventory"
)

func TestStartBuildsDeterministicEvidenceDrivenItems(t *testing.T) {
	root := t.TempDir()
	alpha := inventory.Skill{Name: "α-skill", Root: root, EncounteredPath: filepath.Join(root, "α-skill")}
	beta := inventory.Skill{Name: "beta", Root: root, EncounteredPath: filepath.Join(root, "beta")}
	findings := []analysis.Finding{
		{ID: "unseen:beta", Type: analysis.FindingUnseen, Severity: 4, Title: "beta", Skills: []inventory.Skill{beta}, Reasons: []string{"no strong or medium invocation evidence found in opted-in history"}},
		{ID: "duplicate:alpha", Type: analysis.FindingDuplicate, Severity: 1, Title: "α-skill", Skills: []inventory.Skill{alpha}, Reasons: []string{"identical content in two active roots"}},
		{ID: "tokens:beta", Type: analysis.FindingHighTokenCost, Severity: 3, Title: "beta", Skills: []inventory.Skill{beta}, Reasons: []string{"large"}},
	}

	session := Start(findings, nil, State{})
	if session.Total() != 2 {
		t.Fatalf("total=%d want 2", session.Total())
	}
	item, ok := session.Current()
	if !ok || item.Finding.ID != "duplicate:alpha" || item.Target.EncounteredPath != alpha.EncounteredPath {
		t.Fatalf("first item=%#v, ok=%v", item, ok)
	}
	if item.Consequence == "" || len(item.Evidence) == 0 {
		t.Fatalf("item lacks evidence or consequence: %#v", item)
	}
}

func TestIncompleteScopeResumesAndMissingInstallsCountAsResolved(t *testing.T) {
	root := t.TempDir()
	alpha := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "alpha")}
	beta := inventory.Skill{Name: "beta", Root: root, EncounteredPath: filepath.Join(root, "beta")}
	findings := []analysis.Finding{{ID: "conflict:x", Type: analysis.FindingConflict, Severity: 1, Title: "x", Skills: []inventory.Skill{alpha, beta}, Reasons: []string{"different content"}}}

	session := Start(findings, nil, State{})
	first, _ := session.Current()
	next, err := session.Decide(ActionRevisit)
	if err != nil {
		t.Fatal(err)
	}
	resumed := Start(findings, nil, next.State())
	current, ok := resumed.Current()
	if !ok || current.ID == first.ID {
		t.Fatalf("did not resume at next item: %#v", current)
	}

	withoutFirst := []analysis.Finding{{ID: "conflict:x", Type: analysis.FindingConflict, Severity: 1, Title: "x", Skills: []inventory.Skill{beta}, Reasons: []string{"different content"}}}
	resumed = Start(withoutFirst, nil, next.State())
	if resumed.Reviewed() != 1 || resumed.Total() != 2 {
		t.Fatalf("progress=%d/%d want 1/2", resumed.Reviewed(), resumed.Total())
	}
}

func TestRevisitReturnsInNewReviewButKeptSkillDoesNot(t *testing.T) {
	root := t.TempDir()
	alpha := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "alpha")}
	finding := analysis.Finding{ID: "unseen:alpha", Type: analysis.FindingUnseen, Severity: 4, Title: "alpha", Skills: []inventory.Skill{alpha}, Reasons: []string{"not observed in opted-in history"}}

	session := Start([]analysis.Finding{finding}, nil, State{})
	deferred, err := session.Decide(ActionRevisit)
	if err != nil {
		t.Fatal(err)
	}
	if !deferred.Complete() {
		t.Fatal("single deferred item should complete the active scope")
	}
	again := Start([]analysis.Finding{finding}, nil, deferred.State())
	if _, ok := again.Current(); !ok {
		t.Fatal("deferred item did not return in a new review")
	}
	kept := Start([]analysis.Finding{finding}, []string{"alpha"}, deferred.State())
	if kept.Total() != 0 || !kept.Complete() {
		t.Fatalf("kept skill should not return: %#v", kept.State())
	}
}

func TestEmptyReviewIsScopedComplete(t *testing.T) {
	session := Start(nil, nil, State{})
	if !session.Complete() || session.Total() != 0 {
		t.Fatalf("empty session=%#v", session.State())
	}
	if session.CompletionMessage() != "Review scope complete. This does not mean the global inventory is clean." {
		t.Fatalf("completion=%q", session.CompletionMessage())
	}
}
