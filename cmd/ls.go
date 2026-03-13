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

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List agent sessions",
	RunE:  runLs,
}

func init() {
	rootCmd.AddCommand(lsCmd)
}

func runLs(_ *cobra.Command, _ []string) error {
	sessions, err := session.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tMODEL\tSTATUS\tAGE\tTASK")
	for _, s := range sessions {
		status := "done"
		if tmux.SessionExists(s.TmuxSession) {
			status = "running"
		}
		age := time.Since(s.StartedAt).Round(time.Second)
		task := s.Task
		if len(task) > 60 {
			task = task[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Model, status, age, task)
	}
	return w.Flush()
}
