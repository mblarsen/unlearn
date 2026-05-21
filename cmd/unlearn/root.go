package unlearn

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mblarsen/unlearn/internal/actions"
	"github.com/mblarsen/unlearn/internal/analysis"
	"github.com/mblarsen/unlearn/internal/config"
	"github.com/mblarsen/unlearn/internal/history"
	"github.com/mblarsen/unlearn/internal/inventory"
	"github.com/mblarsen/unlearn/internal/llm"
	setupflow "github.com/mblarsen/unlearn/internal/setup"
	"github.com/mblarsen/unlearn/internal/state"
	"github.com/mblarsen/unlearn/internal/tui"
	"github.com/mblarsen/unlearn/internal/ui"
	"github.com/mblarsen/unlearn/internal/usage"
	"github.com/spf13/cobra"
)

type cliOptions struct {
	roots           []string
	trustRoots      []string
	writeRoots      []string
	configPath      string
	stateDir        string
	indexPath       string
	fix             bool
	yes             bool
	restoreRoot     string
	historyJSONL    []string
	historySQLite   []string
	historyCacheTTL time.Duration
	rescanSources   bool
	withLLM         bool
	activeAgents    []string
	inactiveAgents  []string
	warnings        []string
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
			skills, findings, err := runLoadingInventory(out, opts)
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
			progress := newAuditProgressPrinter(cmd.ErrOrStderr())
			skills, findings, skipped, err := loadInventoryWithOptions(opts, inventoryLoadOptions{Progress: progress.Update})
			if err != nil {
				return err
			}
			if opts.fix {
				printWarnings(out, opts.warnings)
				return runFix(out, opts, findings)
			}
			printWarnings(out, opts.warnings)
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
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			skills, findings, skipped, err := loadInventoryWithOptions(opts, inventoryLoadOptions{Context: ctx, HistoryProgress: func(progress history.ScanProgress) {
				if progress.Done {
					fmt.Fprintf(out, "History scanned: %s (%d lines, %d skills with derived evidence)\n", progress.Path, progress.Lines, progress.Matches)
				}
			}})
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
			printWarnings(out, opts.warnings)
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

	reset := &cobra.Command{
		Use:   "reset",
		Short: "Remove local unlearn config and cache state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(out, cmd.InOrStdin(), opts)
		},
	}
	addSharedFlags(reset, opts)
	reset.Flags().BoolVarP(&opts.yes, "yes", "y", false, "confirm reset without prompting")

	resetLLMSummary := &cobra.Command{
		Use:   "llm-summary <content-hash-or-skill-name>",
		Short: "Remove one cached LLM summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResetLLMSummary(out, cmd.InOrStdin(), opts, args[0])
		},
	}
	addSharedFlags(resetLLMSummary, opts)
	resetLLMSummary.Flags().BoolVarP(&opts.yes, "yes", "y", false, "confirm reset without prompting")
	reset.AddCommand(resetLLMSummary)
	root.AddCommand(reset)

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

type loadingResultMsg struct {
	skills   []inventory.Skill
	findings []analysis.Finding
	err      error
}

type loadingProgressMsg struct {
	event inventoryProgress
}

type loadingTickMsg time.Time

type loadingModel struct {
	width     int
	height    int
	frame     int
	status    string
	detail    string
	updates   <-chan tea.Msg
	result    loadingResultMsg
	cancelled bool
}

func newLoadingModel(updates <-chan tea.Msg) loadingModel {
	return loadingModel{status: "Preparing inventory", detail: "Checking local dashboard cache", updates: updates}
}

func (m loadingModel) Init() tea.Cmd {
	return tea.Batch(loadingTickCmd(), waitForLoadingUpdate(m.updates))
}

func loadingTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return loadingTickMsg(t) })
}

func waitForLoadingUpdate(updates <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-updates
	}
}

func (m loadingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		}
	case loadingProgressMsg:
		m.status = progressLabel(msg.event.Step)
		m.detail = loadingProgressDetail(msg.event)
		if msg.event.Done {
			m.detail = "complete — " + m.detail
		}
		return m, waitForLoadingUpdate(m.updates)
	case loadingResultMsg:
		m.result = msg
		return m, tea.Quit
	case loadingTickMsg:
		m.frame++
		return m, loadingTickCmd()
	}
	return m, nil
}

func (m loadingModel) View() string {
	width := m.width
	if width <= 0 {
		width = 90
	}
	height := m.height
	if height <= 0 {
		height = 25
	}
	barWidth := min(36, max(12, width-24))
	filled := (m.frame % (barWidth + 1))
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	theme := tuiThemeForLoading()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 3).
		Width(min(72, width-8)).
		Render(strings.Join([]string{
			theme.status.Render(m.status),
			theme.bar.Render(bar),
			theme.detail.Render(ui.Truncate(m.detail, min(64, width-16))),
			theme.detail.Render("press q to cancel"),
		}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

type loadingTheme struct{ status, bar, detail lipgloss.Style }

func tuiThemeForLoading() loadingTheme {
	return loadingTheme{
		status: lipgloss.NewStyle().Foreground(lipgloss.Color("255")),
		bar:    lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		detail: lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	}
}

func runLoadingInventory(out io.Writer, opts *cliOptions) ([]inventory.Skill, []analysis.Finding, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	updates := make(chan tea.Msg, 16)
	sendProgress := func(event inventoryProgress) {
		select {
		case updates <- loadingProgressMsg{event: event}:
		default:
		}
	}
	go func() {
		skills, findings, err := loadDashboardInventory(opts, inventoryLoadOptions{Context: ctx, Progress: sendProgress, HistoryProgress: func(progress history.ScanProgress) {
			detail := fmt.Sprintf("%s · %d lines · %d matching skills", progress.Path, progress.Lines, progress.Matches)
			if progress.Done {
				detail = fmt.Sprintf("%s · complete · %d lines · %d matching skills", progress.Path, progress.Lines, progress.Matches)
			}
			sendProgress(inventoryProgress{Step: "history", Detail: detail, Done: progress.Done})
		}})
		updates <- loadingResultMsg{skills: skills, findings: findings, err: err}
	}()
	program := tea.NewProgram(newLoadingModel(updates), tea.WithOutput(out), tea.WithAltScreen())
	finalModel, err := program.Run()
	stop()
	if err != nil {
		return nil, nil, err
	}
	loading, ok := finalModel.(loadingModel)
	if !ok {
		return nil, nil, fmt.Errorf("loading returned unexpected model %T", finalModel)
	}
	if loading.cancelled {
		return nil, nil, fmt.Errorf("loading cancelled")
	}
	return loading.result.skills, loading.result.findings, loading.result.err
}

func loadingProgressDetail(event inventoryProgress) string {
	detail := strings.TrimSpace(event.Detail)
	if event.Total > 0 {
		prefix := fmt.Sprintf("%d/%d", event.Current, event.Total)
		if detail == "" {
			return prefix
		}
		return prefix + " — " + detail
	}
	return detail
}

func loadDashboardInventory(opts *cliOptions, loadOpts inventoryLoadOptions) ([]inventory.Skill, []analysis.Finding, error) {
	if canUseDashboardCache(opts) {
		paths, err := pathsFromOptions(opts)
		if err != nil {
			return nil, nil, err
		}
		if err := paths.Ensure(); err != nil {
			return nil, nil, err
		}
		db, err := state.OpenIndex(paths.IndexPath)
		if err != nil {
			return nil, nil, err
		}
		defer db.Close()
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "load-cache", Detail: "local dashboard index"})
		skills, findings, err := state.LoadInventoryCache(db)
		if err == nil {
			reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "load-cache", Detail: fmt.Sprintf("%d skills, %d findings", len(skills), len(findings)), Done: true})
			return skills, findings, nil
		}
	}
	skills, findings, _, err := loadInventoryWithOptions(opts, loadOpts)
	if err != nil {
		return nil, nil, err
	}
	if err := saveDashboardInventory(opts, skills, findings); err != nil {
		return nil, nil, err
	}
	return skills, findings, nil
}

func canUseDashboardCache(opts *cliOptions) bool {
	return !opts.rescanSources && len(opts.roots) == 0 && len(opts.trustRoots) == 0 && len(opts.writeRoots) == 0 && len(opts.historyJSONL) == 0 && len(opts.historySQLite) == 0 && len(opts.activeAgents) == 0 && len(opts.inactiveAgents) == 0 && !opts.withLLM
}

func saveDashboardInventory(opts *cliOptions, skills []inventory.Skill, findings []analysis.Finding) error {
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
	return state.ReplaceIndex(db, skills, findings)
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
	statuses := inventory.AgentStatuses()
	roots := setupflow.CandidateRoots(opts.roots, opts.activeAgents, opts.inactiveAgents, cfg, statuses)
	choices := setupflow.RootChoices(roots)
	historyJSONL, err := usage.DiscoverPiJSONL()
	if err != nil {
		return err
	}
	historySQLite, err := usage.DiscoverSQLite(roots)
	if err != nil {
		return err
	}
	model := setupflow.New(choices, historyJSONL, historySQLite, cfg, statuses)
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

func addSharedFlags(cmd *cobra.Command, opts *cliOptions) {
	cmd.Flags().StringSliceVar(&opts.roots, "root", nil, "skill root to scan; may be repeated")
	cmd.Flags().StringSliceVar(&opts.trustRoots, "trust-root", nil, "trust a skill root for this run and persist the decision")
	cmd.Flags().StringSliceVar(&opts.writeRoots, "write-root", nil, "allow modifications in this trusted root and persist the decision")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "config TOML path")
	cmd.Flags().StringVar(&opts.stateDir, "state-dir", "", "state directory for index, quarantine, and caches")
	cmd.Flags().StringVar(&opts.indexPath, "index", "", "SQLite index path")
	cmd.Flags().StringSliceVar(&opts.historyJSONL, "history-jsonl", nil, "opt-in JSONL history file to scan for derived invocation evidence; may be repeated")
	cmd.Flags().StringSliceVar(&opts.historySQLite, "history-sqlite", nil, "opt-in SQLite history database to scan text columns for derived invocation evidence; may be repeated")
	cmd.Flags().DurationVar(&opts.historyCacheTTL, "history-cache-ttl", 24*time.Hour, "reuse cached history evidence until it is older than this duration")
	cmd.Flags().BoolVar(&opts.rescanSources, "rescan-sources", false, "ignore cached source/history evidence and rescan local sources")
	cmd.Flags().BoolVar(&opts.withLLM, "with-llm", false, "opt in to Gemini-assisted semantic overlap analysis; uses GEMINI_API_KEY or GOOGLE_API_KEY")
	cmd.Flags().StringSliceVar(&opts.activeAgents, "active-agent", nil, "active agent harness whose global skill roots should be audited; may be repeated")
	cmd.Flags().StringSliceVar(&opts.inactiveAgents, "inactive-agent", nil, "inactive agent harness whose skill roots should be scanned as cleanup candidates; may be repeated")
}

type inventoryLoadOptions struct {
	Context         context.Context
	HistoryProgress func(history.ScanProgress)
	Progress        func(inventoryProgress)
}

func loadInventory(opts *cliOptions) ([]inventory.Skill, []analysis.Finding, []string, error) {
	return loadInventoryWithOptions(opts, inventoryLoadOptions{})
}

func loadInventoryWithOptions(opts *cliOptions, loadOpts inventoryLoadOptions) ([]inventory.Skill, []analysis.Finding, []string, error) {
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
	activeAgents, inactiveAgents := agentSelection(opts, cfg)
	roots := opts.roots
	if len(roots) == 0 {
		roots = inventory.RootsForAgents(append(activeAgents, inactiveAgents...))
		if len(roots) == 0 {
			roots = inventory.KnownGlobalRoots()
		}
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
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "scan-roots", Detail: fmt.Sprintf("%d trusted root(s), %d skipped", len(scanRoots), len(skipped))})
	owners := inventory.RootOwnershipForAgents(activeAgents, inactiveAgents)
	report, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: scanRoots, RootOwnerships: owners})
	if err != nil {
		return nil, nil, nil, err
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "scan-roots", Detail: fmt.Sprintf("%d skill install(s)", len(report.Skills)), Done: true})
	usageResult, err := usage.Load(usage.Options{
		Config:          cfg,
		Paths:           paths,
		Skills:          report.Skills,
		TrustedRoots:    scanRoots,
		HistoryJSONL:    opts.historyJSONL,
		HistorySQLite:   opts.historySQLite,
		HistoryCacheTTL: opts.historyCacheTTL,
		RescanSources:   opts.rescanSources,
		Context:         loadOpts.Context,
		Progress: func(event usage.Progress) {
			reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: event.Step, Current: event.Current, Total: event.Total, Detail: event.Detail, Done: event.Done})
		},
		HistoryProgress: loadOpts.HistoryProgress,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	skills := usageResult.Skills
	if skills == nil {
		skills = report.Skills
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "analysis", Detail: "duplicates, conflicts, keywords, safety findings"})
	analysisOpts := analysis.Options{UsageEvidence: usageResult.Evidence, Progress: func(event analysis.ProgressEvent) {
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: event.Step, Current: event.Current, Total: event.Total, Detail: event.Detail, Done: event.Done})
	}}
	var recorder *recordingAnalyzer
	if cfg.LLMAssisted {
		if analyzer, ok := llm.NewGeminiAnalyzerFromEnv(); ok {
			recorder = newRecordingAnalyzer(llm.NewCachedAnalyzer(paths.LLMCacheDir, analyzer))
			analysisOpts.LLMAnalyzer = recorder
		} else {
			opts.warnings = append(opts.warnings, "LLM analysis requested, but GEMINI_API_KEY/GOOGLE_API_KEY is not set; using deterministic analysis.")
		}
	}
	ctx := loadOpts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	findings, err := analysis.AnalyzeWithLLM(ctx, skills, analysisOpts)
	if err != nil {
		opts.warnings = append(opts.warnings, fmt.Sprintf("LLM semantic-overlap analysis did not complete; using deterministic analysis. Details: %v", err))
		findings = analysis.Analyze(skills, analysis.Options{UsageEvidence: usageResult.Evidence})
	}
	if recorder != nil {
		skills = attachLLMSummaries(skills, recorder.Summaries())
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "analysis", Detail: fmt.Sprintf("%d finding(s)", len(findings)), Done: true})
	return skills, findings, skipped, nil
}

type recordingAnalyzer struct {
	next      llm.Analyzer
	summaries map[string]llm.GeneratedSummary
}

func newRecordingAnalyzer(next llm.Analyzer) *recordingAnalyzer {
	return &recordingAnalyzer{next: next, summaries: map[string]llm.GeneratedSummary{}}
}

func (a *recordingAnalyzer) Summarize(ctx context.Context, name, deterministicSummary, contentHash string) (llm.GeneratedSummary, error) {
	summary, err := a.next.Summarize(ctx, name, deterministicSummary, contentHash)
	if err == nil && strings.TrimSpace(contentHash) != "" {
		a.summaries[contentHash] = summary
	}
	return summary, err
}

func (a *recordingAnalyzer) FindOverlaps(ctx context.Context, summaries []llm.GeneratedSummary) ([]llm.SemanticOverlap, error) {
	return a.next.FindOverlaps(ctx, summaries)
}

func (a *recordingAnalyzer) Summaries() map[string]llm.GeneratedSummary {
	return a.summaries
}

func attachLLMSummaries(skills []inventory.Skill, summaries map[string]llm.GeneratedSummary) []inventory.Skill {
	if len(summaries) == 0 {
		return skills
	}
	enriched := append([]inventory.Skill(nil), skills...)
	for i := range enriched {
		summary, ok := summaries[enriched[i].ContentHash]
		if !ok || strings.TrimSpace(summary.Summary) == "" || isDisabledLLMSummary(summary.Provider, summary.Model) {
			continue
		}
		enriched[i].LLMSummary = summary.Summary
		enriched[i].LLMProvider = summary.Provider
		enriched[i].LLMModel = summary.Model
	}
	return enriched
}

func isDisabledLLMSummary(provider, model string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "disabled") && strings.EqualFold(strings.TrimSpace(model), "disabled")
}

func reportInventoryProgress(progress func(inventoryProgress), event inventoryProgress) {
	if progress != nil {
		progress(event)
	}
}

func agentSelection(opts *cliOptions, cfg config.Config) ([]string, []string) {
	return setupflow.SelectAgentIDs(opts.activeAgents, opts.inactiveAgents, cfg, inventory.AgentStatuses())
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
	if len(opts.activeAgents) > 0 {
		cfg.ActiveAgents = append([]string(nil), opts.activeAgents...)
		changed = true
	}
	if len(opts.inactiveAgents) > 0 {
		cfg.InactiveAgents = append([]string(nil), opts.inactiveAgents...)
		changed = true
	}
	if opts.withLLM && !cfg.LLMAssisted {
		cfg.LLMAssisted = true
		changed = true
	}
	if len(opts.historyJSONL) > 0 || len(opts.historySQLite) > 0 {
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
		for _, path := range opts.historySQLite {
			if !containsString(cfg.HistorySQLite, path) {
				cfg.HistorySQLite = append(cfg.HistorySQLite, path)
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

func runReset(out io.Writer, in io.Reader, opts *cliOptions) error {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "unlearn reset will remove local unlearn config and cache state.")
	fmt.Fprintf(out, "  remove config: %s\n", paths.ConfigPath)
	fmt.Fprintf(out, "  remove SQLite index: %s\n", paths.IndexPath)
	fmt.Fprintf(out, "  remove LLM cache: %s\n", paths.LLMCacheDir)
	fmt.Fprintf(out, "  keep quarantine: %s\n", paths.QuarantineDir)
	fmt.Fprintln(out, "Skill roots and quarantined skills will not be removed.")
	if !opts.yes {
		fmt.Fprint(out, "Type yes to continue: ")
		answer, err := readResetConfirmation(in)
		if err != nil {
			return err
		}
		if answer != "yes" && answer != "y" {
			fmt.Fprintln(out, "Reset cancelled.")
			return nil
		}
	}
	removed, err := removeResetTargets(paths)
	if err != nil {
		return err
	}
	if removed == 0 {
		fmt.Fprintln(out, "No local unlearn config or cache state found.")
		return nil
	}
	fmt.Fprintf(out, "Reset complete. Removed %d local unlearn item(s).\n", removed)
	return nil
}

func runResetLLMSummary(out io.Writer, in io.Reader, opts *cliOptions, target string) error {
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("LLM summary target is required")
	}
	matches, err := llmSummaryResetTargets(opts, paths, target)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		fmt.Fprintf(out, "No cached LLM summary found for %q.\n", target)
		return nil
	}
	fmt.Fprintf(out, "unlearn reset llm-summary will remove %d cached summary file(s):\n", len(matches))
	for _, match := range matches {
		fmt.Fprintf(out, "  %s (%s)\n", match.ContentHash, match.Path)
	}
	fmt.Fprintf(out, "  refresh overlap cache: %s\n", llm.OverlapCacheDir(paths.LLMCacheDir))
	if !opts.yes {
		fmt.Fprint(out, "Type yes to continue: ")
		answer, err := readResetConfirmation(in)
		if err != nil {
			return err
		}
		if answer != "yes" && answer != "y" {
			fmt.Fprintln(out, "LLM summary reset cancelled.")
			return nil
		}
	}
	removed := 0
	removedHashes := make([]string, 0, len(matches))
	for _, match := range matches {
		ok, err := removeResetTarget(match.Path)
		if err != nil {
			return err
		}
		if ok {
			removed++
			removedHashes = append(removedHashes, match.ContentHash)
		}
	}
	overlapRemoved, err := removeResetTarget(llm.OverlapCacheDir(paths.LLMCacheDir))
	if err != nil {
		return err
	}
	if removed == 0 {
		fmt.Fprintln(out, "No cached LLM summary files were present by the time reset ran.")
		return nil
	}
	sort.Strings(removedHashes)
	fmt.Fprintf(out, "Removed %d cached LLM summary file(s) for content hash(es): %s.\n", removed, strings.Join(removedHashes, ", "))
	if overlapRemoved {
		fmt.Fprintln(out, "Removed overlap cache so the next LLM-assisted scan recomputes semantic overlaps with refreshed summaries.")
	} else {
		fmt.Fprintln(out, "No overlap cache found; future overlap cache keys include summary/content inputs and will refresh as those inputs change.")
	}
	return nil
}

type llmSummaryResetTarget struct {
	ContentHash string
	Path        string
}

func llmSummaryResetTargets(opts *cliOptions, paths state.Paths, target string) ([]llmSummaryResetTarget, error) {
	byHash := map[string]llmSummaryResetTarget{}
	addExistingHash := func(hash string) {
		path := llm.SummaryCachePath(paths.LLMCacheDir, hash)
		if _, err := os.Stat(path); err == nil {
			byHash[hash] = llmSummaryResetTarget{ContentHash: hash, Path: path}
		}
	}
	addExistingHash(target)

	cfg, err := loadConfig(opts, paths)
	if err != nil {
		return nil, err
	}
	activeAgents, inactiveAgents := agentSelection(opts, cfg)
	roots := opts.roots
	if len(roots) == 0 {
		roots = inventory.RootsForAgents(append(activeAgents, inactiveAgents...))
		if len(roots) == 0 {
			roots = inventory.KnownGlobalRoots()
		}
	}
	scanRoots := cfg.TrustedRoots(roots)
	if len(scanRoots) == 0 {
		return sortedLLMSummaryResetTargets(byHash), nil
	}
	owners := inventory.RootOwnershipForAgents(activeAgents, inactiveAgents)
	report, err := inventory.NewScanner().Scan(inventory.ScanOptions{Roots: scanRoots, RootOwnerships: owners})
	if err != nil {
		return nil, err
	}
	for _, skill := range report.Skills {
		if !strings.EqualFold(strings.TrimSpace(skill.Name), target) {
			continue
		}
		addExistingHash(skill.ContentHash)
	}
	return sortedLLMSummaryResetTargets(byHash), nil
}

func sortedLLMSummaryResetTargets(values map[string]llmSummaryResetTarget) []llmSummaryResetTarget {
	out := make([]llmSummaryResetTarget, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ContentHash < out[j].ContentHash })
	return out
}

func readResetConfirmation(in io.Reader) (string, error) {
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(scanner.Text())), nil
}

func removeResetTargets(paths state.Paths) (int, error) {
	removed := 0
	for _, path := range resetTargetPaths(paths) {
		ok, err := removeResetTarget(path)
		if err != nil {
			return removed, err
		}
		if ok {
			removed++
		}
	}
	_ = removeEmptyDir(paths.BaseDir)
	_ = removeEmptyDir(filepath.Dir(paths.ConfigPath))
	return removed, nil
}

func resetTargetPaths(paths state.Paths) []string {
	return uniquePaths([]string{
		paths.ConfigPath,
		paths.IndexPath,
		paths.IndexPath + "-wal",
		paths.IndexPath + "-shm",
		paths.LLMCacheDir,
	})
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "." || seen[clean] {
			continue
		}
		seen[clean] = true
		unique = append(unique, clean)
	}
	return unique
}

func removeResetTarget(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, os.RemoveAll(path)
}

func removeEmptyDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	return os.Remove(path)
}

func printWarnings(out io.Writer, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}
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
	for _, typ := range []analysis.FindingType{analysis.FindingDuplicate, analysis.FindingConflict, analysis.FindingOverlap, analysis.FindingBroken, analysis.FindingInactiveRoot, analysis.FindingHighTokenCost, analysis.FindingBroadActivation, analysis.FindingUnseen} {
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
