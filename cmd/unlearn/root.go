package unlearn

import (
	"context"
	"database/sql"
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
	progress history.ScanProgress
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
	return loadingModel{status: "Scanning skill roots", detail: "Building inventory and derived evidence", updates: updates}
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
		m.status = "Scanning history evidence"
		m.detail = fmt.Sprintf("%s · %d lines · %d matching skills", msg.progress.Path, msg.progress.Lines, msg.progress.Matches)
		if msg.progress.Done {
			m.detail = fmt.Sprintf("%s · complete · %d lines · %d matching skills", msg.progress.Path, msg.progress.Lines, msg.progress.Matches)
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
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
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
			theme.title.Render(spinner[m.frame%len(spinner)] + " unlearn is loading"),
			theme.status.Render(m.status),
			theme.bar.Render(bar),
			theme.detail.Render(ui.Truncate(m.detail, min(64, width-16))),
			theme.detail.Render("press q to cancel"),
		}, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

type loadingTheme struct{ title, status, bar, detail lipgloss.Style }

func tuiThemeForLoading() loadingTheme {
	return loadingTheme{
		title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		status: lipgloss.NewStyle().Foreground(lipgloss.Color("255")),
		bar:    lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		detail: lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	}
}

func runLoadingInventory(out io.Writer, opts *cliOptions) ([]inventory.Skill, []analysis.Finding, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	updates := make(chan tea.Msg, 16)
	go func() {
		skills, findings, _, err := loadInventoryWithOptions(opts, inventoryLoadOptions{Context: ctx, HistoryProgress: func(progress history.ScanProgress) {
			select {
			case updates <- loadingProgressMsg{progress: progress}:
			default:
			}
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
	activeAgents, inactiveAgents := agentSelection(opts, cfg)
	roots := opts.roots
	if len(roots) == 0 {
		roots = inventory.RootsForAgents(append(activeAgents, inactiveAgents...))
		if len(roots) == 0 {
			roots = inventory.KnownGlobalRoots()
		}
	}
	choices := make([]setupflow.RootChoice, 0, len(roots))
	for _, root := range roots {
		_, err := os.Stat(root)
		choices = append(choices, setupflow.RootChoice{Path: root, Exists: err == nil})
	}
	historyJSONL, err := discoverPiHistoryJSONL()
	if err != nil {
		return err
	}
	historySQLite, err := discoverHistorySQLite(roots)
	if err != nil {
		return err
	}
	model := setupflow.New(choices, historyJSONL, historySQLite, cfg, inventory.AgentStatuses())
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

func discoverHistorySQLite(roots []string) ([]string, error) {
	return history.DiscoverSQLite(roots, history.DefaultDiscoveryLimit)
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
	usage, sources, lastSeen, err := loadUsageEvidence(opts, cfg, report.Skills, scanRoots, loadOpts)
	if err != nil {
		return nil, nil, nil, err
	}
	skills := attachUsageEvidence(report.Skills, usage, sources, lastSeen)
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "analysis", Detail: "duplicates, conflicts, keywords, safety findings"})
	analysisOpts := analysis.Options{UsageEvidence: usage, Progress: func(event analysis.ProgressEvent) {
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: event.Step, Current: event.Current, Total: event.Total, Detail: event.Detail, Done: event.Done})
	}}
	if cfg.LLMAssisted {
		if analyzer, ok := llm.NewGeminiAnalyzerFromEnv(); ok {
			analysisOpts.LLMAnalyzer = llm.NewCachedAnalyzer(paths.LLMCacheDir, analyzer)
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
		findings = analysis.Analyze(skills, analysis.Options{UsageEvidence: usage})
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "analysis", Detail: fmt.Sprintf("%d finding(s)", len(findings)), Done: true})
	return skills, findings, skipped, nil
}

func reportInventoryProgress(progress func(inventoryProgress), event inventoryProgress) {
	if progress != nil {
		progress(event)
	}
}

func agentSelection(opts *cliOptions, cfg config.Config) ([]string, []string) {
	if len(opts.activeAgents) > 0 || len(opts.inactiveAgents) > 0 {
		return append([]string(nil), opts.activeAgents...), append([]string(nil), opts.inactiveAgents...)
	}
	if cfg.HasAgentSelection() {
		return append([]string(nil), cfg.ActiveAgents...), append([]string(nil), cfg.InactiveAgents...)
	}
	return inventory.CandidateAgentIDs()
}

func loadUsageEvidence(opts *cliOptions, cfg config.Config, skills []inventory.Skill, scanRoots []string, loadOpts inventoryLoadOptions) (analysis.UsageEvidence, map[string][]string, map[string]time.Time, error) {
	jsonlPaths := opts.historyJSONL
	sqlitePaths := opts.historySQLite
	if len(jsonlPaths) == 0 && len(sqlitePaths) == 0 && cfg.HistoryScan {
		jsonlPaths = cfg.HistoryJSONL
		sqlitePaths = cfg.HistorySQLite
	}
	if cfg.HistoryScan && len(opts.historyJSONL) == 0 && len(opts.historySQLite) == 0 {
		discoveredSQLite, err := discoverHistorySQLite(scanRoots)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, path := range discoveredSQLite {
			sqlitePaths = appendUnique(sqlitePaths, path)
		}
	}
	if len(jsonlPaths) == 0 && len(sqlitePaths) == 0 {
		return nil, nil, nil, nil
	}
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	usage := analysis.UsageEvidence{}
	sources := map[string][]string{}
	lastSeen := map[string]time.Time{}
	paths, err := pathsFromOptions(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := paths.Ensure(); err != nil {
		return nil, nil, nil, err
	}
	db, err := state.OpenIndex(paths.IndexPath)
	if err != nil {
		return nil, nil, nil, err
	}
	defer db.Close()
	merge := func(evidence []history.Evidence) {
		for _, item := range evidence {
			current := usage[item.SkillName]
			if current == "" || evidenceRank(item.Grade) < evidenceRank(history.EvidenceGrade(current)) {
				usage[item.SkillName] = string(item.Grade)
			}
			sources[item.SkillName] = appendUnique(sources[item.SkillName], item.Source)
			if item.SeenAt.After(lastSeen[item.SkillName]) {
				lastSeen[item.SkillName] = item.SeenAt
			}
		}
	}
	jsonlAdapter := history.JSONLAdapter{}
	for index, path := range jsonlPaths {
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "history", Current: index + 1, Total: len(jsonlPaths) + len(sqlitePaths), Detail: filepath.Base(path)})
		evidence, err := historyEvidenceForPath(db, path, names, opts, loadOpts, func(path string, names []string, scanOpts history.ScanOptions) ([]history.Evidence, error) {
			return jsonlAdapter.ScanWithOptions(path, names, scanOpts)
		})
		if err != nil {
			return nil, nil, nil, err
		}
		merge(evidence)
	}
	sqliteAdapter := history.SQLiteAdapter{}
	for index, path := range sqlitePaths {
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "history", Current: len(jsonlPaths) + index + 1, Total: len(jsonlPaths) + len(sqlitePaths), Detail: filepath.Base(path)})
		evidence, err := historyEvidenceForPath(db, path, names, opts, loadOpts, func(path string, names []string, scanOpts history.ScanOptions) ([]history.Evidence, error) {
			return sqliteAdapter.ScanWithOptions(path, names, scanOpts)
		})
		if err != nil {
			return nil, nil, nil, err
		}
		merge(evidence)
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "history", Detail: fmt.Sprintf("%d file(s), %d matching skills", len(jsonlPaths)+len(sqlitePaths), len(usage)), Done: true})
	return usage, sources, lastSeen, nil
}

type historyScannerFunc func(path string, names []string, opts history.ScanOptions) ([]history.Evidence, error)

func historyEvidenceForPath(db *sql.DB, path string, names []string, opts *cliOptions, loadOpts inventoryLoadOptions, scan historyScannerFunc) ([]history.Evidence, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if !opts.rescanSources {
		status, err := state.HistoryCacheStatusForSource(db, path, info.ModTime(), opts.historyCacheTTL, now)
		if err != nil {
			return nil, err
		}
		if status.Fresh {
			return state.LoadHistoryEvidence(db, path)
		}
	}
	reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "history", Detail: path})
	historyProgress := func(progress history.ScanProgress) {
		if loadOpts.HistoryProgress != nil {
			loadOpts.HistoryProgress(progress)
		}
		reportInventoryProgress(loadOpts.Progress, inventoryProgress{Step: "history", Detail: fmt.Sprintf("%s · %d lines · %d matching skills", progress.Path, progress.Lines, progress.Matches), Done: progress.Done})
	}
	evidence, err := scan(path, names, history.ScanOptions{Context: loadOpts.Context, Progress: historyProgress})
	if err != nil {
		return nil, err
	}
	if err := state.SaveHistoryEvidence(db, path, info.ModTime(), evidence); err != nil {
		return nil, err
	}
	return evidence, nil
}

func attachUsageEvidence(skills []inventory.Skill, usage analysis.UsageEvidence, sources map[string][]string, lastSeen map[string]time.Time) []inventory.Skill {
	if usage == nil {
		return skills
	}
	enriched := append([]inventory.Skill(nil), skills...)
	for i := range enriched {
		key := strings.ToLower(enriched[i].Name)
		enriched[i].HistoryEvidence = usage[key]
		enriched[i].HistorySources = append([]string(nil), sources[key]...)
		enriched[i].HistoryLastSeenAt = lastSeen[key]
	}
	return enriched
}

func appendUnique(values []string, value string) []string {
	if containsString(values, value) {
		return values
	}
	return append(values, value)
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
