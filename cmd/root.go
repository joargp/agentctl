package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joargp/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "agentctl",
	Short:        "Run and monitor pi coding agent sessions",
	SilenceUsage: true, // don't print usage on runtime errors
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return ensureDirs()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func ensureDirs() error {
	// tmux socket dir
	if err := os.MkdirAll(socketDir(), 0o755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	// data subdirs
	base, err := session.DataDir()
	if err != nil {
		return err
	}
	for _, sub := range []string{"sessions", "logs", "scripts", "runtime"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			return fmt.Errorf("create data dir %s: %w", sub, err)
		}
	}
	return nil
}

func socketDir() string {
	if d := os.Getenv("CLAUDE_TMUX_SOCKET_DIR"); d != "" {
		return d
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "claude-tmux-sockets")
}
