package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
	"github.com/mblarsen/unlearn/internal/state"
)

func TestRunDerivesUnseenEligibilityFromEvidenceCoverage(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	writeSkill(t, root, "beta", "beta body")
	paths := testPaths(t)
	cfg := trustedConfig(root)

	t.Run("unknown without opted-in sources", func(t *testing.T) {
		result, err := Run(context.Background(), Policy{Config: cfg, Paths: paths, Roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if result.EvidenceCoverage != EvidenceUnknown {
			t.Fatalf("coverage=%q", result.EvidenceCoverage)
		}
		assertFindingCount(t, result.Findings, analysis.FindingUnseen, 0)
	})

	t.Run("complete after all selected sources are scanned", func(t *testing.T) {
		historyPath := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(historyPath, []byte(`{"message":"read skills/alpha/SKILL.md"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := Run(context.Background(), Policy{Config: cfg, Paths: paths, Roots: []string{root}, HistoryJSONL: []string{historyPath}})
		if err != nil {
			t.Fatal(err)
		}
		if result.EvidenceCoverage != EvidenceComplete {
			t.Fatalf("coverage=%q", result.EvidenceCoverage)
		}
		assertFindingCount(t, result.Findings, analysis.FindingUnseen, 1)
		if result.Skills[0].HistoryEvidence == "" && result.Skills[1].HistoryEvidence == "" {
			t.Fatal("derived evidence was not attached to inventory")
		}
	})

	t.Run("incomplete when a configured source disappeared", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.jsonl")
		incomplete := cfg
		incomplete.HistoryScan = true
		incomplete.HistoryJSONL = []string{missing}
		result, err := Run(context.Background(), Policy{Config: incomplete, Paths: paths, Roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if result.EvidenceCoverage != EvidenceIncomplete {
			t.Fatalf("coverage=%q", result.EvidenceCoverage)
		}
		assertFindingCount(t, result.Findings, analysis.FindingUnseen, 0)
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != DiagnosticMissingHistorySource || result.Diagnostics[0].Path != missing {
			t.Fatalf("diagnostics=%#v", result.Diagnostics)
		}
	})
}

func TestRunUsesRealSQLiteSnapshotCache(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	policy := Policy{Config: trustedConfig(root), Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh}
	first, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Skills) != 1 || first.FromSnapshotCache {
		t.Fatalf("first=%#v", first)
	}

	writeSkill(t, root, "beta", "beta body")
	policy.SnapshotCache = SnapshotCachePrefer
	second, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Skills) != 1 || !second.FromSnapshotCache {
		t.Fatalf("preferred cache not used: %#v", second)
	}

	policy.SnapshotCache = SnapshotCacheRefresh
	third, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Skills) != 2 || third.FromSnapshotCache {
		t.Fatalf("refresh did not rescan: %#v", third)
	}
}

func TestRunPreferCacheHonorsChangedEvidencePolicyAndForcedRescan(t *testing.T) {
	for _, forceRescan := range []bool{false, true} {
		t.Run(fmt.Sprintf("force-rescan=%t", forceRescan), func(t *testing.T) {
			root := t.TempDir()
			writeSkill(t, root, "alpha", "alpha body")
			historyPath := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(historyPath, []byte(`{"message":"use alpha"}`+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := trustedConfig(root)
			cfg.HistoryScan = true
			cfg.HistoryJSONL = []string{historyPath}
			policy := Policy{Config: cfg, Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh}
			first, err := Run(context.Background(), policy)
			if err != nil {
				t.Fatal(err)
			}
			if first.EvidenceCoverage != EvidenceComplete {
				t.Fatalf("initial coverage=%q", first.EvidenceCoverage)
			}

			policy.Config.HistoryScan = false
			policy.Config.HistoryJSONL = nil
			policy.RescanSources = forceRescan
			policy.SnapshotCache = SnapshotCachePrefer
			second, err := Run(context.Background(), policy)
			if err != nil {
				t.Fatal(err)
			}
			if second.FromSnapshotCache || second.EvidenceCoverage != EvidenceUnknown {
				t.Fatalf("revoked history policy reused cache: %#v", second)
			}
		})
	}
}

func TestRunPreferCacheHonorsRevokedTrust(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	paths := testPaths(t)
	policy := Policy{Config: trustedConfig(root), Paths: paths, Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh}
	if _, err := Run(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	delete(policy.Config.Roots, root)
	policy.SnapshotCache = SnapshotCachePrefer
	result, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.FromSnapshotCache || len(result.Skills) != 0 || len(result.SkippedRoots) != 1 {
		t.Fatalf("revoked trust reused cache: %#v", result)
	}
}

func TestRunPrunesExternallyDeletedCachedInstallAcrossRestart(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "alpha")
	writeSkill(t, root, "alpha", "alpha body")
	policy := Policy{Config: trustedConfig(root), Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh}
	if _, err := Run(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(skillPath); err != nil {
		t.Fatal(err)
	}
	policy.SnapshotCache = SnapshotCachePrefer
	for restart := 1; restart <= 2; restart++ {
		result, err := Run(context.Background(), policy)
		if err != nil {
			t.Fatalf("restart %d: %v", restart, err)
		}
		if len(result.Skills) != 0 || len(result.Findings) != 0 {
			t.Fatalf("restart %d retained stale cache: %#v", restart, result)
		}
	}
}

func TestRunIgnoresInventoryCacheWithoutTypedAuditMetadata(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "actual", "actual body")
	paths := testPaths(t)
	db, err := state.OpenIndex(paths.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	cached := []inventory.Skill{{ID: "legacy", Name: "legacy", Root: root, EncounteredPath: filepath.Join(root, "legacy")}}
	if err := state.ReplaceIndex(db, cached, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Policy{Config: trustedConfig(root), Paths: paths, Roots: []string{root}, SnapshotCache: SnapshotCachePrefer})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "actual" || result.FromSnapshotCache {
		t.Fatalf("legacy cache was not replaced by scan: %#v", result)
	}
}

func TestAttachLLMSummariesSkipsDisabledOutput(t *testing.T) {
	skills := []inventory.Skill{{Name: "alpha", ContentHash: "hash"}}
	got := attachLLMSummaries(skills, map[string]llm.GeneratedSummary{"hash": {Summary: "fallback", Provider: "disabled", Model: "disabled"}})
	if got[0].LLMSummary != "" {
		t.Fatalf("disabled summary attached: %#v", got[0])
	}
}

func TestRunInjectsAndCachesExternalLLMWhileFallingBackDeterministically(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	writeSkill(t, root, "beta", "beta body")
	paths := testPaths(t)
	cfg := trustedConfig(root)
	cfg.LLMAssisted = true

	working := &stubAnalyzer{}
	policy := Policy{Config: cfg, Paths: paths, Roots: []string{root}, LLMAnalyzer: working}
	result, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if working.summaryCalls != 2 {
		t.Fatalf("summary calls=%d", working.summaryCalls)
	}
	for _, skill := range result.Skills {
		if !strings.HasPrefix(skill.LLMSummary, "generated ") {
			t.Fatalf("generated summary not attached: %#v", skill)
		}
	}
	cached := &stubAnalyzer{err: errors.New("external call should be cached")}
	policy.LLMAnalyzer = cached
	result, err = Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if cached.summaryCalls != 0 {
		t.Fatalf("summary cache missed: calls=%d diagnostics=%#v", cached.summaryCalls, result.Diagnostics)
	}

	paths = testPaths(t)
	writeSkill(t, root, "alpha", strings.Repeat("large ", 9000))
	failing := &stubAnalyzer{err: errors.New("provider unavailable")}
	policy.Paths = paths
	policy.LLMAnalyzer = failing
	result, err = Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) == 0 {
		t.Fatal("deterministic fallback returned no findings")
	}
	if !hasDiagnostic(result.Diagnostics, DiagnosticLLMFallback) {
		t.Fatalf("missing fallback diagnostic: %#v", result.Diagnostics)
	}
}

func TestRunCancellationAtAnalysisSeamDoesNotPersistSnapshot(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	paths := testPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Run(ctx, Policy{
		Config: trustedConfig(root), Paths: paths, Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh,
		Progress: func(event Progress) {
			if event.Step == "analysis" && !event.Done {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(paths.IndexPath); !os.IsNotExist(err) {
		t.Fatalf("canceled audit persisted snapshot: %v", err)
	}
}

func TestRunReportsCancellationFromHistoryScan(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	historyPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(historyPath, []byte(strings.Repeat(`{"message":"noise"}`+"\n", 2000)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Policy{Config: trustedConfig(root), Paths: testPaths(t), Roots: []string{root}, HistoryJSONL: []string{historyPath}, RescanSources: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

type stubAnalyzer struct {
	summaryCalls int
	err          error
}

func (a *stubAnalyzer) Summarize(_ context.Context, name, _ string, hash string) (llm.GeneratedSummary, error) {
	a.summaryCalls++
	if a.err != nil {
		return llm.GeneratedSummary{}, a.err
	}
	return llm.GeneratedSummary{Name: name, Summary: "generated " + name, Provider: "stub", Model: "test", ContentHash: hash}, nil
}

func (a *stubAnalyzer) FindOverlaps(context.Context, []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	if a.err != nil {
		return nil, a.err
	}
	return nil, nil
}

func (a *stubAnalyzer) LintSkillQuality(_ context.Context, request llm.SkillQualityRequest) (llm.SkillQualityResult, error) {
	if a.err != nil {
		return llm.SkillQualityResult{}, a.err
	}
	return llm.SkillQualityResult{Provider: "stub", Model: "test", ContentHash: request.ContentHash}, nil
}

func (a *stubAnalyzer) ProviderName() string { return "stub" }
func (a *stubAnalyzer) ModelName() string    { return "test" }

func trustedConfig(root string) config.Config {
	cfg := config.Default()
	cfg.TrustRoot(root)
	return cfg
}

func testPaths(t *testing.T) state.Paths {
	t.Helper()
	base := t.TempDir()
	return state.Paths{BaseDir: base, ConfigPath: filepath.Join(base, "config.toml"), IndexPath: filepath.Join(base, "index.db"), QuarantineDir: filepath.Join(base, "quarantine"), LLMCacheDir: filepath.Join(base, "llm-cache")}
}

func writeSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " description\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFindingCount(t *testing.T, findings []analysis.Finding, typ analysis.FindingType, want int) {
	t.Helper()
	got := 0
	for _, finding := range findings {
		if finding.Type == typ {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s findings=%d want=%d; all=%#v", typ, got, want, findings)
	}
}

func hasDiagnostic(diagnostics []Diagnostic, code DiagnosticCode) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
