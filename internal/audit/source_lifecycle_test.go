package audit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mblarsen/unlearn/internal/analysis"
)

func TestStoppedHistoryNoticeIsNotReplayedBySnapshotOrRescan(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "alpha body")
	cfg := trustedConfig(root)
	cfg.HistoryScan = true
	cfg.HistoryJSONL = []string{filepath.Join(t.TempDir(), "gone.jsonl")}
	policy := Policy{Config: cfg, Paths: testPaths(t), Roots: []string{root}, SnapshotCache: SnapshotCacheRefresh}
	first, err := Run(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Diagnostics) != 1 || !strings.HasPrefix(first.Diagnostics[0].Message, "Stopped tracking missing history source: ") {
		t.Fatalf("notice=%+v", first.Diagnostics)
	}
	for _, mode := range []SnapshotCachePolicy{SnapshotCachePrefer, SnapshotCacheRefresh} {
		policy.SnapshotCache = mode
		for _, rescan := range []bool{false, true} {
			policy.RescanSources = rescan
			next, err := Run(context.Background(), policy)
			if err != nil {
				t.Fatal(err)
			}
			if len(next.Diagnostics) != 0 || next.EvidenceCoverage != EvidenceIncomplete {
				t.Fatalf("mode=%v rescan=%v result=%+v", mode, rescan, next)
			}
			assertFindingCount(t, next.Findings, analysis.FindingUnseen, 0)
		}
	}
}
