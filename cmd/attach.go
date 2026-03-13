package cmd

import (
	"fmt"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach <id>",
	Short: "Attach your terminal to a running agent session",
	Long: `Hands your terminal directly to the tmux session so you can watch or
intervene (e.g. answer a confirmation prompt). Detach with Ctrl+b d.

Examples:
  agentctl attach abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runAttach,
}

func init() {
	rootCmd.AddCommand(attachCmd)
}

func runAttach(_ *cobra.Command, args []string) error {
	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}
	if !tmux.SessionExists(s.TmuxSession) {
		return fmt.Errorf("session %s is no longer running (use 'dump' to see its output)", id)
	}
	return tmux.Attach(s.TmuxSession)
}
