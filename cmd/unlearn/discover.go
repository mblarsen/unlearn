package unlearn

import (
	"fmt"
	"io"
	"strings"

	"github.com/mblarsen/unlearn/internal/audit"
	"github.com/mblarsen/unlearn/internal/discovery"
	"github.com/spf13/cobra"
)

func newDiscoverCmd(out io.Writer, opts *cliOptions) *cobra.Command {
	limit := 10
	cmd := &cobra.Command{
		Use:   "discover <task>",
		Short: "Find installed skills that match a task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runAudit(opts, inventoryLoadOptions{}, audit.SnapshotCachePrefer)
			if err != nil {
				return err
			}
			printWarnings(cmd.ErrOrStderr(), opts.warnings)
			printDiscovery(out, discovery.Search(strings.Join(args, " "), result.Skills, result.Findings), limit)
			return nil
		},
	}
	addSharedFlags(cmd, opts)
	cmd.Flags().IntVar(&limit, "limit", limit, "maximum number of matching skills to print")
	return cmd
}

func printDiscovery(out io.Writer, result discovery.Result, limit int) {
	fmt.Fprintf(out, "Task: %s\n", result.Query)
	if len(result.Matches) == 0 {
		fmt.Fprintln(out, result.Message)
		return
	}
	shown := len(result.Matches)
	if limit > 0 && shown > limit {
		shown = limit
	}
	fmt.Fprintf(out, "%d matching installed %s", len(result.Matches), skillWord(len(result.Matches)))
	if shown < len(result.Matches) {
		fmt.Fprintf(out, "; showing %d", shown)
	}
	fmt.Fprintln(out)
	for _, match := range result.Matches[:shown] {
		fmt.Fprintf(out, "\n%s\n", match.Name)
		if match.Weak {
			fmt.Fprintln(out, "  Weak term match: only one distinctive task term matched.")
		}
		for _, reason := range match.Reasons {
			fmt.Fprintf(out, "  - %s\n", reason)
		}
		fmt.Fprintf(out, "  Invocation: %s\n", match.Invocation)
		if len(match.OverlapWith) > 0 {
			fmt.Fprintf(out, "  Known overlap in these results: %s\n", strings.Join(match.OverlapWith, ", "))
		}
		for _, install := range match.Installs {
			fmt.Fprintf(out, "  Install: %s\n", install.Path)
			status := "available in inventory"
			if install.Missing {
				status = "missing at scan time"
			}
			fmt.Fprintf(out, "    Inventory status: %s\n", status)
			fmt.Fprintf(out, "    %s\n", discoveryInstallAccess(install))
		}
	}
	fmt.Fprintln(out, "\nMatches use observed names and descriptions. They do not activate skills or prove task completion.")
}

func discoveryInstallAccess(install discovery.Install) string {
	if !install.AccessKnown {
		return "Agent access: unknown"
	}
	if len(install.ActiveAgents) > 0 {
		return "Accessible to: " + strings.Join(install.ActiveAgents, ", ")
	}
	if len(install.InactiveAgents) > 0 {
		return "Known only for inactive agents: " + strings.Join(install.InactiveAgents, ", ")
	}
	return "No selected agent has known access"
}

func skillWord(count int) string {
	if count == 1 {
		return "skill"
	}
	return "skills"
}
