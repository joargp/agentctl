package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Spawn a pi agent session",
	Long: `Spawn pi with the given model and task inside a tmux session.

Prints the session ID to stdout immediately, then returns (or blocks with --wait).
The agent's full output is streamed to a log file via tmux pipe-pane.

Examples:
  agentctl run --model claude-4.6 --task "add tests for the auth module"
  agentctl run --model o3 --task "review this PR" --cwd /repos/myapp --wait`,
	RunE: runRun,
}

var (
	runModel string
	runTask  string
	runCwd   string
	runName  string
	runWait  bool
)

func init() {
	runCmd.Flags().StringVar(&runModel, "model", "", "model to pass to pi (required)")
	runCmd.Flags().StringVar(&runTask, "task", "", "task prompt (required)")
	runCmd.Flags().StringVar(&runCwd, "cwd", "", "working directory (default: current dir)")
	runCmd.Flags().StringVar(&runName, "name", "", "short label for monitor output (default: model name)")
	runCmd.Flags().BoolVar(&runWait, "wait", false, "block until the agent session finishes")
	_ = runCmd.MarkFlagRequired("model")
	_ = runCmd.MarkFlagRequired("task")
	rootCmd.AddCommand(runCmd)
}

func runRun(_ *cobra.Command, _ []string) error {
	id, err := session.NewID()
	if err != nil {
		return err
	}

	cwd := runCwd
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return err
		}
	}

	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "agentctl")
	logFile := filepath.Join(dataDir, "logs", id+".log")
	taskFile := filepath.Join(dataDir, "scripts", id+".task")
	scriptFile := filepath.Join(dataDir, "scripts", id+".sh")
	tmuxSession := "agentctl-" + id

	// Write task to a file so the shell script can read it safely,
	// avoiding any quoting issues with the task content.
	if err := os.WriteFile(taskFile, []byte(runTask), 0o644); err != nil {
		return fmt.Errorf("write task file: %w", err)
	}

	// Wrapper script: cd, read task, exec pi (exec so session dies when pi exits).
	script := fmt.Sprintf(`#!/bin/sh
set -e
cd %s
task=$(cat %s)
exec pi --model %s --no-session -p "$task"
`, shellQuote(cwd), shellQuote(taskFile), shellQuote(runModel))
	if err := os.WriteFile(scriptFile, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write script: %w", err)
	}

	// Ensure log file exists before pipe-pane (cat >> needs the parent dir).
	if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}

	if err := tmux.EnsureSocket(); err != nil {
		return err
	}
	// Start the session directly running the script — no outer shell.
	// When pi exits the script exits, the window closes, and the session is destroyed.
	if err := tmux.NewSession(tmuxSession, "sh", scriptFile); err != nil {
		return err
	}
	if err := tmux.PipePaneToFile(tmuxSession, logFile); err != nil {
		return err
	}

	sess := &session.Session{
		ID:          id,
		Name:        runName,
		Model:       runModel,
		Task:        runTask,
		Cwd:         cwd,
		TmuxSession: tmuxSession,
		LogFile:     logFile,
		ScriptFile:  scriptFile,
		TaskFile:    taskFile,
		StartedAt:   time.Now(),
	}
	if err := session.Save(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	// ID goes to stdout so callers can capture it cleanly.
	fmt.Println(id)

	// Hints go to stderr so they don't pollute captured output.
	fmt.Fprintf(os.Stderr, "model:  %s\n", runModel)
	fmt.Fprintf(os.Stderr, "log:    %s\n", logFile)
	fmt.Fprintf(os.Stderr, "\nTo monitor:  agentctl monitor %s\n", id)
	fmt.Fprintf(os.Stderr, "To attach:   agentctl attach %s\n", id)

	if runWait {
		fmt.Fprintln(os.Stderr, "\nWaiting for agent to finish...")
		for tmux.SessionExists(tmuxSession) {
			time.Sleep(500 * time.Millisecond)
		}
		fmt.Fprintln(os.Stderr, "done.")
	}

	return nil
}

// shellQuote wraps s in single quotes, escaping any existing single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
