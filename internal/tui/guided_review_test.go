package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/review"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestGuidedReviewKeepPersistsAndCompletesScope(t *testing.T) {
	root := t.TempDir()
	skill := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "alpha")}
	finding := reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)
	service := &fakeActionService{}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{finding}, service)
	m.Width, m.Height = 100, 30

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	if m.Mode != ViewGuidedReview || !strings.Contains(m.View(), "Not observed does not mean unused") {
		t.Fatalf("guided review did not open:\n%s", m.View())
	}
	updated, _ = m.Update(key("k"))
	m = updated.(Model)
	if len(service.kept) != 1 || service.reviewSaves != 2 || !m.GuidedReview.Complete() {
		t.Fatalf("keep was not saved: kept=%v state=%#v", service.kept, service.reviewState)
	}
	if !strings.Contains(m.View(), "does not mean the global inventory is clean") {
		t.Fatalf("completion scope is unclear:\n%s", m.View())
	}
}

func TestGuidedReviewKeepProtectsTheLogicalSkillAcrossCurrentScope(t *testing.T) {
	root := t.TempDir()
	one := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "one", "alpha")}
	two := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "two", "alpha")}
	finding := analysis.Finding{ID: "duplicate:alpha", Type: analysis.FindingDuplicate, Severity: 1, Title: "alpha", Skills: []inventory.Skill{one, two}, Reasons: []string{"identical content"}}
	service := &fakeActionService{}
	m := NewWithActions([]inventory.Skill{one, two}, []analysis.Finding{finding}, service)

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	updated, _ = m.Update(key("k"))
	m = updated.(Model)
	if !m.GuidedReview.Complete() || m.GuidedReview.Reviewed() != 2 {
		t.Fatalf("logical keep did not resolve both installs: %d/%d", m.GuidedReview.Reviewed(), m.GuidedReview.Total())
	}
}

func TestGuidedReviewRevisitReturnsOnlyInLaterReview(t *testing.T) {
	skill := inventory.Skill{Name: "alpha", Root: t.TempDir(), EncounteredPath: "/tmp/alpha"}
	service := &fakeActionService{}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)}, service)
	m.Width, m.Height = 100, 30

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	updated, _ = m.Update(key("l"))
	m = updated.(Model)
	if !m.GuidedReview.Complete() || service.reviewState.Decisions[0].Action != review.ActionRevisit {
		t.Fatalf("revisit was not persisted: %#v", service.reviewState)
	}
	updated, _ = m.Update(key("esc"))
	m = updated.(Model)
	updated, _ = m.Update(key("v"))
	m = updated.(Model)
	if _, ok := m.GuidedReview.Current(); !ok {
		t.Fatal("deferred item did not return in the next review")
	}
}

func TestGuidedReviewQuarantineUsesExactTargetAndConfirmation(t *testing.T) {
	root := t.TempDir()
	one := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "one", "alpha")}
	two := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "two", "alpha")}
	service := &fakeActionService{writeRoots: map[string]bool{root: true}}
	finding := analysis.Finding{ID: "duplicate:alpha", Type: analysis.FindingDuplicate, Severity: 1, Title: "alpha", Skills: []inventory.Skill{two, one}, Reasons: []string{"identical effective content"}}
	m := NewWithActions([]inventory.Skill{one, two}, []analysis.Finding{finding}, service)

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	current, _ := m.GuidedReview.Current()
	updated, _ = m.Update(key("q"))
	m = updated.(Model)
	if m.State != StateConfirmQuarantine || len(m.PendingSkills) != 1 || m.PendingSkills[0].EncounteredPath != current.Target.EncounteredPath {
		t.Fatalf("quarantine did not use exact review target: state=%v targets=%#v", m.State, m.PendingSkills)
	}
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.quarantined) != 1 || service.reviewState.Decisions[0].Action != review.ActionQuarantine {
		t.Fatalf("quarantine decision not saved: quarantined=%v state=%#v", service.quarantined, service.reviewState)
	}
}

func TestGuidedReviewRecordsCompletedTargetFromPartialQuarantineOutcome(t *testing.T) {
	root := t.TempDir()
	skill := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "alpha")}
	service := &fakeActionService{writeRoots: map[string]bool{root: true}, quarantineErr: os.ErrPermission}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)}, service)

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	updated, _ = m.Update(key("q"))
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if len(service.reviewState.Decisions) != 1 || service.reviewState.Decisions[0].Action != review.ActionQuarantine {
		t.Fatalf("completed partial target was not recorded: %#v", service.reviewState)
	}
	if !strings.Contains(m.Status, "error") {
		t.Fatalf("partial failure was hidden: %q", m.Status)
	}
}

func TestGuidedReviewQuarantinePersistsConfigAndSQLiteSnapshot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "skills")
	install := filepath.Join(root, "alpha")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "SKILL.md"), []byte("---\nname: alpha\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := inventory.Skill{Name: "alpha", Kind: inventory.KindDirectory, Root: root, EncounteredPath: install, PrimaryPath: filepath.Join(install, "SKILL.md")}
	finding := reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)
	configPath := filepath.Join(base, "config.toml")
	indexPath := filepath.Join(base, "index.db")
	cfg := config.Default()
	cfg.TrustRoot(root)
	cfg.AllowWrite(root)
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	db, err := state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ReplaceIndex(db, []inventory.Skill{skill}, []analysis.Finding{finding}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	service := &ConfigActionService{ConfigPath: configPath, Config: cfg, IndexPath: indexPath, QuarantineDir: filepath.Join(base, "quarantine")}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{finding}, service)

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	updated, _ = m.Update(key("q"))
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if _, err := os.Stat(install); !os.IsNotExist(err) {
		t.Fatalf("install still exists: %v", err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.GuidedReview.Decisions) != 1 || loaded.GuidedReview.Decisions[0].Action != review.ActionQuarantine {
		t.Fatalf("config review state=%#v", loaded.GuidedReview)
	}
	db, err = state.OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	skills, findings, err := state.LoadInventoryCache(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 || len(findings) != 0 {
		t.Fatalf("index was not reconciled: skills=%#v findings=%#v", skills, findings)
	}
}

func TestGuidedReviewCancelDoesNotRecordDecision(t *testing.T) {
	root := t.TempDir()
	skill := inventory.Skill{Name: "alpha", Root: root, EncounteredPath: filepath.Join(root, "alpha")}
	service := &fakeActionService{writeRoots: map[string]bool{root: true}}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingConflict, "conflict:alpha", skill)}, service)

	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	updated, _ = m.Update(key("q"))
	m = updated.(Model)
	updated, _ = m.Update(key("esc"))
	m = updated.(Model)
	if len(service.reviewState.Decisions) != 0 || m.Mode != ViewGuidedReview {
		t.Fatalf("cancel changed review: state=%#v mode=%v", service.reviewState, m.Mode)
	}
}

func TestGuidedReviewContentIsScrollableAndLongPathIsReachable(t *testing.T) {
	longTail := strings.Repeat("長", 90) + "PATH-END"
	skill := inventory.Skill{Name: "alpha", Root: "/tmp", EncounteredPath: "/tmp/" + longTail}
	m := New([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)})
	m.Width, m.Height = 80, 18
	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	m.setStatus("saved the previous decision")
	seen := m.View()
	for i := 0; i < 50; i++ {
		updated, _ = m.Update(key("down"))
		m = updated.(Model)
		seen += "\n" + m.View()
	}
	if !strings.Contains(seen, "Consequence") || !strings.Contains(seen, "PATH-END") {
		t.Fatalf("review content is not fully reachable:\n%s", seen)
	}
}

func TestGuidedReviewRoutesFeedbackAndInterruptControls(t *testing.T) {
	skill := inventory.Skill{Name: "alpha", Root: "/tmp", EncounteredPath: "/tmp/alpha"}
	m := New([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)})
	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	m.setStatus("error: " + strings.Repeat("long error ", 80) + "RECOVERY-END")
	updated, _ = m.Update(key("enter"))
	m = updated.(Model)
	if m.State != StateFeedback {
		t.Fatal("enter did not open advertised feedback details")
	}
	updated, _ = m.Update(key("esc"))
	m = updated.(Model)
	updated, _ = m.Update(key("x"))
	m = updated.(Model)
	if m.Status != "" {
		t.Fatal("x did not dismiss guided review feedback")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did not return a quit command")
	}
}

func TestConfigActionServiceFailedReviewSaveDoesNotMutateMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	service := &ConfigActionService{ConfigPath: path, Config: config.Default()}
	initial := review.State{Scope: []review.ScopeItem{{ID: "one", FindingID: "unseen:alpha", SkillName: "alpha", InstallPath: "/tmp/alpha"}}}
	if err := service.SaveGuidedReviewState(initial); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := initial
	candidate.Decisions = []review.Decision{{ItemID: "one", Action: review.ActionRevisit}}
	if err := service.SaveGuidedReviewState(candidate); err == nil {
		t.Fatal("expected save failure")
	}
	saved, _ := service.GuidedReviewState()
	if len(saved.Decisions) != 0 {
		t.Fatalf("failed save mutated in-memory config: %#v", saved)
	}
}

func TestGuidedReviewFailedDecisionSaveRemainsRetryable(t *testing.T) {
	skill := inventory.Skill{Name: "alpha", Root: "/tmp", EncounteredPath: "/tmp/alpha"}
	service := &fakeActionService{}
	m := NewWithActions([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:alpha", skill)}, service)
	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	service.reviewSaveErr = os.ErrPermission
	updated, _ = m.Update(key("l"))
	m = updated.(Model)
	if _, ok := m.GuidedReview.Current(); !ok {
		t.Fatal("failed save advanced the running review")
	}
	if len(service.reviewState.Decisions) != 0 {
		t.Fatalf("failed save mutated service state: %#v", service.reviewState)
	}
	service.reviewSaveErr = nil
	updated, _ = m.Update(key("l"))
	m = updated.(Model)
	if !m.GuidedReview.Complete() {
		t.Fatal("decision was not retryable after save recovered")
	}
}

func TestGuidedReviewRendersBoundedAtSupportedSizes(t *testing.T) {
	skill := inventory.Skill{Name: "技能-α", Root: "/tmp/skills", EncounteredPath: "/tmp/skills/非常に長い技能-α"}
	m := New([]inventory.Skill{skill}, []analysis.Finding{reviewFinding(analysis.FindingUnseen, "unseen:技能-α", skill)})
	updated, _ := m.Update(key("v"))
	m = updated.(Model)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 18}, {Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		updated, _ = m.Update(size)
		m = updated.(Model)
		view := m.View()
		if len(strings.Split(view, "\n")) > size.Height {
			t.Fatalf("%dx%d review is too tall", size.Width, size.Height)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("%dx%d review overflows: %q", size.Width, size.Height, line)
			}
		}
	}
}

func reviewFinding(typ analysis.FindingType, id string, skills ...inventory.Skill) analysis.Finding {
	reason := "different effective content"
	if typ == analysis.FindingUnseen {
		reason = "no strong or medium invocation evidence found in opted-in history"
	}
	return analysis.Finding{ID: id, Type: typ, Severity: 1, Title: skills[0].Name, Skills: skills, Reasons: []string{reason}}
}
