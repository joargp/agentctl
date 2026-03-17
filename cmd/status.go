package cmd

import (
	"fmt"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:               "status [id]",
	Short:             "Show a one-line summary of what an agent is currently doing",
	ValidArgsFunction: completeSessionIDs,
	Long: `Parses the JSON log to determine the agent's current activity.
Shows what tool is running, if the agent is thinking, or writing text.
Without an ID, shows status for all running sessions.

Examples:
  agentctl status abc123
  agentctl status`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runStatusAll()
	}

	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	printSessionStatus(s)
	return nil
}

func runStatusAll() error {
	sessions, err := session.List()
	if err != nil {
		return err
	}

	found := false
	for _, s := range sessions {
		if tmux.SessionExists(s.TmuxSession) {
			printSessionStatus(s)
			found = true
		}
	}
	if !found {
		fmt.Println("no running sessions")
	}
	return nil
}

func printSessionStatus(s *session.Session) {
	running := tmux.SessionExists(s.TmuxSession)
	age := time.Since(s.StartedAt).Round(time.Second)

	state := "unknown"
	detail := ""

	// Only read tail of log for performance on large files.
	data := readTail(s.LogFile, 64*1024)
	if len(data) > 0 {
		state, detail = session.ParseLastActivity(data)
	} else if running {
		state = "starting"
	}

	statusLabel := "done"
	if running {
		statusLabel = "running"
	}

	label := s.Label()
	if detail != "" {
		fmt.Printf("%s  %s  %s  %s  %s: %s\n", s.ID, label, statusLabel, age, state, detail)
	} else {
		fmt.Printf("%s  %s  %s  %s  %s\n", s.ID, label, statusLabel, age, state)
	}
}
