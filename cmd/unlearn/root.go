package unlearn

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	setupflow "github.com/mblarsen/unlearn/internal/setup"
	"github.com/mblarsen/unlearn/internal/state"
	"github.com/mblarsen/unlearn/internal/tui"
	"github.com/spf13/cobra"
)

type cliOptions struct {
	roots        []string
	trustRoots   []string
	writeRoots   []string
	configPath   string
	stateDir     string
	indexPath    string
	fix          bool
	yes          bool
	restoreRoot  string
	historyJSONL []string
	withLLM      bool
}

func Execute() error {
	return newRootCmd(os.Stdout).Execute()
}

func newRootCmd(out io.Writer) *cobra.Command {
	opts := &cliOptions{}
	root := &cobra.Command{
		Use:   "unlearn",
		Short: "Audit and clean up global AI-agent skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runFirstLaunchSetup(out, opts); err != nil {
				return err
			}
			skills, findings, _, err := loadInventory(opts)
			if err != nil {
				return err
			}
			paths, err := pathsFromOptions(opts)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(opts, paths)
			if err != nil {
				return err
			}
			service := &tui.ConfigActionService{ConfigPath: paths.ConfigPath, Config: cfg, QuarantineDir: paths.QuarantineDir}
			program := tea.NewProgram(tui.NewWithActions(skills, findings, service), tea.WithOutput(out), tea.WithAltScreen())
			_, err = program.Run()
			return err
		},
	}
	addSharedFlags(root, opts)

	audit := &cobra.Command{
		Use:   "audit",
		Short: "Print a concise read-only skill cleanup overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, findings, skipped, err := loadInventory(opts)
			if err != nil {
				return err
			}
			if opts.fix {
				return runFix(out, opts, findings)
			}
			printAudit(out, skills, findings, skipped)
			return nil
		},
	}
	addSharedFlags(audit, opts)
	audit.Flags().BoolVar(&opts.fix, "fix", false, "preview safe quick fixes and apply only after confirmation")
	audit.Flags().BoolVarP(&opts.yes, "yes", "y", false, "confirm safe quick fixes for automation")
	root.AddCommand(audit)

	scan := &cobra.Command{
		Use:   "scan",
		Short: "Refresh the local inventory index",
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, findings, skipped, err := loadInventory(opts)
			if err != nil {
				return err
			}
			paths, err := pathsFromOptions(opts)
			if err != nil {
				return err
			}
			if err := paths.Ensure(); err != nil {
				return err
			}
			db, err := state.OpenIndex(paths.IndexPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := state.ReplaceIndex(db, skills, findings); err != nil {
				return err
			}
			fmt.Fprintf(out, "Indexed %d skills and %d findings.\n", len(skills), len(findings))
			if len(skipped) > 0 {
				fmt.Fprintf(out, "Skipped untrusted roots: %s\n", strings.Join(skipped, ", "))
			}
			return nil
		},
	}
	addSharedFlags(scan, opts)
	root.AddCommand(scan)

	restore := &cobra.Command{
		Use:   "restore <skill>",
		Short: "Restore a quarantined skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := pathsFromOptions(opts)
			if err != nil {
				return err
			}
			destRoot := opts.restoreRoot
			if destRoot == "" {
				return fmt.Errorf("--to-root is required for restore in this safety-first build")
			}
			cfg, err := loadConfig(opts, paths)
			if err != nil {
				return err
			}
			if !cfg.CanWrite(destRoot) {
				return fmt.Errorf("write permission required for restore root %s; pass --write-root %s", destRoot, destRoot)
			}
			mgr := actions.Manager{Config: cfg, QuarantineDir: paths.QuarantineDir}
			dest, err := mgr.Restore(args[0], destRoot)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Restored %s to %s\n", args[0], dest)
			return nil
		},
	}
	addSharedFlags(restore, opts)
	restore.Flags().StringVar(&opts.restoreRoot, "to-root", "", "trusted/write-enabled root to restore into")
	root.AddCommand(restore)

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Run the first-launch setup screen again",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupScreen(out, opts, true)
		},
	}
	addSharedFlags(setupCmd, opts)
	root.AddCommand(setupCmd)

	return root
}

func runFirstLaunchSetup(out io.Writer, opts *cliOptions) error {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts, paths)
	if err != nil {
		return err
	}
	if cfg.SetupComplete {
		return nil
	}
	return runSetupScreen(out, opts, false)
}

func runSetupScreen(out io.Writer, opts *cliOptions, force bool) error {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	cfg, err := loadConfig(opts, paths)
	if err != nil {
		return err
	}
	if cfg.SetupComplete && !force {
		return nil
	}
	roots := opts.roots
	if len(roots) == 0 {
		roots = inventory.KnownGlobalRoots()
	}
	choices := make([]setupflow.RootChoice, 0, len(roots))
	for _, root := range roots {
		_, err := os.Stat(root)
		choices = append(choices, setupflow.RootChoice{Path: root, Exists: err == nil})
	}
	historyPaths, err := discoverPiHistoryJSONL()
	if err != nil {
		return err
	}
	model := setupflow.New(choices, historyPaths, cfg)
	program := tea.NewProgram(model, tea.WithOutput(out), tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		return err
	}
	finalSetup, ok := finalModel.(setupflow.Model)
	if !ok {
		return fmt.Errorf("setup returned unexpected model %T", finalModel)
	}
	if finalSetup.Cancelled {
		return fmt.Errorf("setup cancelled")
	}
	updated := finalSetup.ApplyTo(cfg)
	return updated.Save(paths.ConfigPath)
}

func discoverPiHistoryJSONL() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return history.DiscoverPiJSONL(home, history.DefaultDiscoveryLimit)
}

func addSharedFlags(cmd *cobra.Command, opts *cliOptions) {
	cmd.Flags().StringSliceVar(&opts.roots, "root", nil, "skill root to scan; may be repeated")
	cmd.Flags().StringSliceVar(&opts.trustRoots, "trust-root", nil, "trust a skill root for this run and persist the decision")
	cmd.Flags().StringSliceVar(&opts.writeRoots, "write-root", nil, "allow modifications in this trusted root and persist the decision")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "config TOML path")
	cmd.Flags().StringVar(&opts.stateDir, "state-dir", "", "state directory for index, quarantine, and caches")
	cmd.Flags().StringVar(&opts.indexPath, "index", "", "SQLite index path")
	cmd.Flags().StringSliceVar(&opts.historyJSONL, "history-jsonl", nil, "opt-in JSONL history file to scan for derived invocation evidence; may be repeated")
	cmd.Flags().BoolVar(&opts.withLLM, "with-llm", false, "opt in to LLM-assisted analysis plumbing; current build uses deterministic analysis plus a disabled analyzer stub")
}

func loadInventory(opts *cliOptions) ([]inventory.Skill, []analysis.Finding, []string, error) {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := paths.Ensure(); err != nil {
		return nil, nil, nil, err
	}
	cfg, err := loadConfig(opts, paths)
	if err != nil {
		return nil, nil, nil, err
	}
	roots := opts.roots
	if len(roots) == 0 {
		roots = inventory.KnownGlobalRoots()
	}
	var scanRoots []string
	var skipped []string
	for _, root := range roots {
		if cfg.IsTrusted(root) {
			scanRoots = append(scanRoots, root)
		} else {
			skipped = append(skipped, root)
		}
	}
	report, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: scanRoots})
	if err != nil {
		return nil, nil, nil, err
	}
	usage, err := loadUsageEvidence(opts, cfg, report.Skills)
	if err != nil {
		return nil, nil, nil, err
	}
	findings := analysis.Analyze(report.Skills, analysis.Options{UsageEvidence: usage})
	return report.Skills, findings, skipped, nil
}

func loadUsageEvidence(opts *cliOptions, cfg config.Config, skills []inventory.Skill) (analysis.UsageEvidence, error) {
	historyPaths := opts.historyJSONL
	if len(historyPaths) == 0 && cfg.HistoryScan {
		historyPaths = cfg.HistoryJSONL
	}
	if len(historyPaths) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	usage := analysis.UsageEvidence{}
	adapter := history.JSONLAdapter{}
	for _, path := range historyPaths {
		evidence, err := adapter.Scan(path, names)
		if err != nil {
			return nil, err
		}
		for _, item := range evidence {
			current := usage[item.SkillName]
			if current == "" || evidenceRank(item.Grade) < evidenceRank(history.EvidenceGrade(current)) {
				usage[item.SkillName] = string(item.Grade)
			}
		}
	}
	return usage, nil
}

func evidenceRank(grade history.EvidenceGrade) int {
	switch grade {
	case history.EvidenceStrong:
		return 1
	case history.EvidenceMedium:
		return 2
	case history.EvidenceWeak:
		return 3
	default:
		return 99
	}
}

func loadConfig(opts *cliOptions, paths state.Paths) (config.Config, error) {
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		return cfg, err
	}
	changed := false
	for _, root := range opts.trustRoots {
		cfg.TrustRoot(root)
		changed = true
	}
	for _, root := range opts.writeRoots {
		cfg.TrustRoot(root)
		cfg.AllowWrite(root)
		changed = true
	}
	if opts.withLLM && !cfg.LLMAssisted {
		cfg.LLMAssisted = true
		changed = true
	}
	if len(opts.historyJSONL) > 0 {
		if !cfg.HistoryScan {
			cfg.HistoryScan = true
			changed = true
		}
		for _, path := range opts.historyJSONL {
			if !containsString(cfg.HistoryJSONL, path) {
				cfg.HistoryJSONL = append(cfg.HistoryJSONL, path)
				changed = true
			}
		}
	}
	if changed {
		if err := cfg.Save(paths.ConfigPath); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func pathsFromOptions(opts *cliOptions) (state.Paths, error) {
	paths, err := state.DefaultPaths()
	if err != nil {
		return paths, err
	}
	if opts.stateDir != "" {
		paths.BaseDir = opts.stateDir
		paths.IndexPath = opts.stateDir + string(os.PathSeparator) + "index.db"
		paths.QuarantineDir = opts.stateDir + string(os.PathSeparator) + "quarantine"
		paths.LLMCacheDir = opts.stateDir + string(os.PathSeparator) + "llm-cache"
	}
	if opts.configPath != "" {
		paths.ConfigPath = opts.configPath
	}
	if opts.indexPath != "" {
		paths.IndexPath = opts.indexPath
	}
	return paths, nil
}

func printAudit(out io.Writer, skills []inventory.Skill, findings []analysis.Finding, skipped []string) {
	fmt.Fprintln(out, "unlearn audit")
	fmt.Fprintf(out, "Skills scanned: %d\n", len(skills))
	if len(skipped) > 0 {
		sort.Strings(skipped)
		fmt.Fprintf(out, "Skipped untrusted roots: %s\n", strings.Join(skipped, ", "))
	}
	counts := analysis.FindingCounts(findings)
	fmt.Fprintln(out, "Findings:")
	for _, typ := range []analysis.FindingType{analysis.FindingDuplicate, analysis.FindingConflict, analysis.FindingOverlap, analysis.FindingBroken, analysis.FindingHighTokenCost, analysis.FindingBroadActivation, analysis.FindingUnseen} {
		fmt.Fprintf(out, "  %s: %d\n", typ, counts[typ])
	}
	fmt.Fprintln(out, "Top cleanup candidates:")
	limit := 5
	if len(findings) < limit {
		limit = len(findings)
	}
	if limit == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for i := 0; i < limit; i++ {
			fmt.Fprintf(out, "  - %s: %s\n", findings[i].Type, findings[i].Title)
		}
	}
	fmt.Fprintln(out, "Open `unlearn` for the dashboard-first cleanup workbench.")
}

func runFix(out io.Writer, opts *cliOptions, findings []analysis.Finding) error {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts, paths)
	if err != nil {
		return err
	}
	plan := buildSafeFixPlan(cfg, findings)
	fmt.Fprintln(out, "unlearn audit --fix dry run")
	if len(plan.Operations) == 0 && len(plan.Skipped) == 0 {
		fmt.Fprintln(out, "No safe quick fixes available.")
		return nil
	}
	for _, op := range plan.Operations {
		fmt.Fprintf(out, "  will %s: %s (%s)\n", op.Action, op.Skill.Name, op.Reason)
	}
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(out, "  skipped: %s\n", skipped)
	}
	if len(plan.Operations) == 0 {
		fmt.Fprintln(out, "No changes made.")
		return nil
	}
	if !opts.yes {
		fmt.Fprintln(out, "No changes made. Re-run with --yes after reviewing the dry run.")
		return nil
	}
	mgr := actions.Manager{Config: cfg, QuarantineDir: paths.QuarantineDir}
	for _, op := range plan.Operations {
		dest, err := mgr.Quarantine(op.Skill, true)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "  quarantined %s -> %s\n", op.Skill.Name, dest)
	}
	return nil
}

type safeFixPlan struct {
	Operations []safeFixOperation
	Skipped    []string
}

type safeFixOperation struct {
	Action string
	Skill  inventory.Skill
	Reason string
}

func buildSafeFixPlan(cfg config.Config, findings []analysis.Finding) safeFixPlan {
	plan := safeFixPlan{}
	for _, finding := range findings {
		switch finding.Type {
		case analysis.FindingDuplicate:
			for i, skill := range finding.Skills {
				if i == 0 {
					continue
				}
				if skill.IsSymlink || skill.ReadOnly || skill.Broken {
					plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s is symlinked, read-only, or broken; dashboard review required", skill.Name))
					continue
				}
				if !cfg.CanWrite(skill.Root) {
					plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s requires --write-root %s", skill.Name, skill.Root))
					continue
				}
				plan.Operations = append(plan.Operations, safeFixOperation{Action: "quarantine exact duplicate", Skill: skill, Reason: "same name and identical effective content; first instance kept"})
			}
		case analysis.FindingBroken:
			for _, skill := range finding.Skills {
				if !skill.Broken {
					plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s has broken references; edit manually from dashboard", skill.Name))
					continue
				}
				if !cfg.CanWrite(skill.Root) {
					plan.Skipped = append(plan.Skipped, fmt.Sprintf("broken symlink %s requires --write-root %s", skill.Name, skill.Root))
					continue
				}
				plan.Operations = append(plan.Operations, safeFixOperation{Action: "quarantine broken symlink", Skill: skill, Reason: "encountered path cannot be resolved"})
			}
		}
	}
	return plan
}
