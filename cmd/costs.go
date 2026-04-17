package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var costsCmd = &cobra.Command{
	Use:   "costs",
	Short: "Show total API costs across all sessions",
	Long: `Sums up API costs from all session JSON logs and displays per-session
and total costs.

Examples:
  agentctl costs
  agentctl costs --since 1d`,
	RunE: runCosts,
}

var costsSince string

func init() {
	costsCmd.Flags().StringVar(&costsSince, "since", "", "show only sessions started within this duration (e.g. 1h, 2d)")
	rootCmd.AddCommand(costsCmd)
}

func runCosts(_ *cobra.Command, _ []string) error {
	sessions, err := session.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	var sinceFilter time.Duration
	if costsSince != "" {
		var err error
		sinceFilter, err = parseDuration(costsSince)
		if err != nil {
			return fmt.Errorf("invalid --since duration: %w", err)
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tMODEL\tAGE\tCOST")

	var totalCost float64
	count := 0
	modelCosts := make(map[string]float64)
	modelCounts := make(map[string]int)
	for _, s := range sessions {
		if sinceFilter > 0 && time.Since(s.StartedAt) > sinceFilter {
			continue
		}
		running := tmux.SessionExists(s.TmuxSession)
		stats := getSessionLogStats(s, running)
		totalCost += stats.TotalCost
		age := time.Since(s.StartedAt).Round(time.Second)
		count++
		model := session.NormalizeModelName(s.Model)

		modelCosts[model] += stats.TotalCost
		modelCounts[model]++

		costStr := ""
		if stats.TotalCost > 0 {
			costStr = fmt.Sprintf("$%.4f", stats.TotalCost)
		}
		label := s.Name
		if label == "" {
			label = model
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, label, model, age, costStr)
	}

	fmt.Fprintf(w, "\t\t\t\t\n")

	// Per-model breakdown
	if len(modelCosts) > 1 {
		for model, cost := range modelCosts {
			fmt.Fprintf(w, "\t\t%s\t%d sessions\t$%.4f\n", model, modelCounts[model], cost)
		}
		fmt.Fprintf(w, "\t\t\t\t\n")
	}

	fmt.Fprintf(w, "TOTAL\t\t%d sessions\t\t$%.4f\n", count, totalCost)
	return w.Flush()
}
