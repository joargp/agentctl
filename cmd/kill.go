package cmd

import (
	"fmt"
	"os"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var killCmd = &cobra.Command{
	Use:   "kill <id>",
	Short: "Kill an agent session and clean up its files",
	Long: `Kills the tmux session and removes the session metadata.
Log files are preserved so you can still run 'dump' afterwards.

Examples:
  agentctl kill abc123`,
	Args: cobra.MaximumNArgs(1),
	RunE: runKill,
}

var killAll bool

func init() {
	killCmd.Flags().BoolVar(&killAll, "all", false, "kill all sessions")
	rootCmd.AddCommand(killCmd)
}

func runKill(_ *cobra.Command, args []string) error {
	if killAll {
		return killAllSessions()
	}
	if len(args) == 0 {
		return fmt.Errorf("provide a session ID or use --all")
	}
	return killOne(args[0])
}

func killOne(id string) error {
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}
	if tmux.SessionExists(s.TmuxSession) {
		if err := tmux.KillSession(s.TmuxSession); err != nil {
			fmt.Fprintf(os.Stderr, "warn: kill tmux session: %v\n", err)
		}
	}
	// Remove script/task files but keep the log.
	for _, f := range []string{s.ScriptFile, s.TaskFile} {
		if f != "" {
			_ = os.Remove(f)
		}
	}
	if err := session.Remove(id); err != nil {
		fmt.Fprintf(os.Stderr, "warn: remove session metadata: %v\n", err)
	}
	fmt.Printf("killed %s\n", id)
	return nil
}

func killAllSessions() error {
	sessions, err := session.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}
	for _, s := range sessions {
		if err := killOne(s.ID); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
		}
	}
	return nil
}
