package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/joargp/agentctl/internal/notify"
	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:    "watch <id>",
	Short:  "Wait for a session to finish then notify a pi session (internal)",
	Hidden: true, // not for direct use; spawned by 'run --notify-session'
	Args:   cobra.ExactArgs(1),
	RunE:   runWatch,
}

var watchNotifySession string

func init() {
	watchCmd.Flags().StringVar(&watchNotifySession, "notify-session", "", "pi session ID to notify on completion")
	_ = watchCmd.MarkFlagRequired("notify-session")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(_ *cobra.Command, args []string) error {
	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("load session %s: %w", id, err)
	}

	// Poll until the tmux session is gone.
	for tmux.SessionExists(s.TmuxSession) {
		time.Sleep(500 * time.Millisecond)
	}

	task := s.Task
	if len(task) > 80 {
		task = task[:77] + "..."
	}

	msg := fmt.Sprintf(
		"Agent **%s** (`%s`) finished.\nTask: %s\n\nRun `agentctl dump %s` to read the output.",
		s.Model, s.ID, task, s.ID,
	)

	if err := notify.SendFollowUp(watchNotifySession, msg); err != nil {
		// Log to stderr — the watcher process is detached so this goes to the OS log.
		fmt.Fprintf(os.Stderr, "agentctl watch: notify failed: %v\n", err)
		os.Exit(1)
	}
	return nil
}
