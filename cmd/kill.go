package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var killCmd = &cobra.Command{
	Use:               "kill <id>",
	Short:             "Kill an agent session and clean up its files",
	ValidArgsFunction: completeSessionIDs,
	Long: `Kills the tmux session and removes the session metadata.
Log files are preserved so you can still run 'dump' afterwards.

Examples:
  agentctl kill abc123`,
	Args: cobra.MaximumNArgs(1),
	RunE: runKill,
}

var (
	killAll   bool
	killClean bool
)

func init() {
	killCmd.Flags().BoolVar(&killAll, "all", false, "kill all sessions")
	killCmd.Flags().BoolVar(&killClean, "clean", false, "also remove log files (default: preserve logs)")
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
	if err := markSessionCancelled(s); err != nil {
		return fmt.Errorf("mark session %s cancelled: %w", id, err)
	}

	var killErrs []error
	if err := cleanupSessionProcessTree(s, runtimeCleanupGrace); err != nil {
		killErrs = append(killErrs, fmt.Errorf("clean up process tree: %w", err))
	}
	if tmux.SessionExists(s.TmuxSession) {
		if err := tmux.KillSession(s.TmuxSession); err != nil {
			killErrs = append(killErrs, fmt.Errorf("kill tmux session: %w", err))
		}
	}
	if len(killErrs) > 0 {
		return fmt.Errorf("kill %s: %w", id, errors.Join(killErrs...))
	}
	// Remove script/task files. Log is preserved unless --clean is set.
	filesToRemove := []string{s.ScriptFile, s.TaskFile, s.RuntimeFile}
	if killClean {
		filesToRemove = append(filesToRemove, s.LogFile, s.LogFile+".stderr")
	}
	for _, f := range filesToRemove {
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
