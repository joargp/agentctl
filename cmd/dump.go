package cmd

import (
	"fmt"
	"os"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:   "dump <id>",
	Short: "Print the last N lines of a session's output",
	Long: `Reads output from the session log file (or live pane if still running).
Useful for feeding agent output back to another LLM.

Examples:
  agentctl dump abc123
  agentctl dump abc123 --lines 200`,
	Args: cobra.ExactArgs(1),
	RunE: runDump,
}

var dumpLines int

func init() {
	dumpCmd.Flags().IntVarP(&dumpLines, "lines", "n", 100, "number of lines to show")
	rootCmd.AddCommand(dumpCmd)
}

func runDump(_ *cobra.Command, args []string) error {
	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	// Prefer live pane capture if session is still running (includes unflushed output).
	if tmux.SessionExists(s.TmuxSession) {
		text, err := tmux.CaptureLast(s.TmuxSession, dumpLines)
		if err == nil {
			fmt.Print(text)
			return nil
		}
		// Fall through to log file on error.
	}

	// Read last N lines from log file.
	data, err := os.ReadFile(s.LogFile)
	if err != nil {
		return fmt.Errorf("read log: %w", err)
	}

	lines := splitLines(data)
	if len(lines) > dumpLines {
		lines = lines[len(lines)-dumpLines:]
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
