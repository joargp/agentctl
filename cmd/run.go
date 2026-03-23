package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

const (
	muninEventsDirEnv = "MUNIN_EVENTS_DIR"
	muninChannelIDEnv = "MUNIN_CHANNEL_ID"
	muninThreadTsEnv  = "MUNIN_THREAD_TS"
	piSessionIDEnv    = "PI_SESSION_ID"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Spawn a pi agent session",
	Long: `Spawn pi with the given model and task inside a tmux session.

Prints the session ID to stdout immediately, then returns (or blocks with --wait).
The agent's output is streamed to a log file with large delta payloads stripped.

Examples:
  agentctl run --model claude-opus-4-6 --task "add tests for the auth module"
  agentctl run --model gpt-5.4 --task-file /tmp/task.txt --cwd /repos/myapp
  agentctl run --model gpt-5.4 --task "review this PR" --cwd /repos/myapp --wait`,
	RunE: runRun,
}

type watcherNotifyOptions struct {
	PiSessionID  string
	EventDir     string
	EventChannel string
	EventThread  string
	Progress     bool
}

var (
	runModel              string
	runTask               string
	runTaskFile           string
	runCwd                string
	runName               string
	runWait               bool
	runNotifySession      string
	runNotifyMunin        bool
	runNotifyEventDir     string
	runNotifyEventChannel string
	runNotifyEventThread  string
)

func init() {
	runCmd.Flags().StringVar(&runModel, "model", "", "model to pass to pi (required)")
	runCmd.Flags().StringVar(&runTask, "task", "", "task prompt (mutually exclusive with --task-file)")
	runCmd.Flags().StringVar(&runTaskFile, "task-file", "", "path to file containing task prompt (mutually exclusive with --task)")
	runCmd.Flags().StringVar(&runCwd, "cwd", "", "working directory (default: current dir)")
	runCmd.Flags().StringVar(&runName, "name", "", "short label for monitor output (default: model name)")
	runCmd.Flags().BoolVar(&runWait, "wait", false, "block until the agent session finishes")
	runCmd.Flags().StringVar(&runNotifySession, "notify-session", "",
		"pi session ID to send a follow_up message to when done (default: $PI_SESSION_ID)")
	runCmd.Flags().BoolVar(&runNotifyMunin, "notify-munin", false,
		"write a Munin-compatible completion event using MUNIN_EVENTS_DIR and MUNIN_CHANNEL_ID (optional MUNIN_THREAD_TS)")
	runCmd.Flags().StringVar(&runNotifyEventDir, "notify-event-dir", "",
		"write an immediate event JSON file to this directory when the agent finishes")
	runCmd.Flags().StringVar(&runNotifyEventChannel, "notify-event-channel", "",
		"channel ID to include in the completion event (requires --notify-event-dir)")
	runCmd.Flags().StringVar(&runNotifyEventThread, "notify-event-thread", "",
		"optional thread ts to include in the completion event (requires --notify-event-dir)")
	rootCmd.AddCommand(runCmd)
}

func runRun(_ *cobra.Command, _ []string) error {
	notifyOptions, err := resolveWatcherNotifyOptions(runNotifyMunin, getenv)
	if err != nil {
		return err
	}
	notifyOptions.Progress = runNotifyMunin

	if runModel == "" {
		return fmt.Errorf("--model is required")
	}

	task, err := resolveRunTask(runTask, runTaskFile)
	if err != nil {
		return err
	}

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

	dataDir, err := session.DataDir()
	if err != nil {
		return err
	}
	logFile := filepath.Join(dataDir, "logs", id+".log")
	taskFile := filepath.Join(dataDir, "scripts", id+".task")
	scriptFile := filepath.Join(dataDir, "scripts", id+".sh")
	tmuxSession := "agentctl-" + id

	// Write task to a file so the shell script can read it safely,
	// avoiding any quoting issues with the task content.
	if err := os.WriteFile(taskFile, []byte(task), 0o644); err != nil {
		return fmt.Errorf("write task file: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Wrapper script: cd, read task, exec pi in JSON mode, mirror raw output to
	// the terminal, and write a sanitized NDJSON stream to the log file. Delta
	// events can otherwise include full accumulated content and make logs grow
	// quadratically.
	script := buildRunScript(cwd, taskFile, runModel, logFile, self)
	if err := os.WriteFile(scriptFile, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write script: %w", err)
	}

	// Ensure log file exists before pi starts writing to it.
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
	// Note: we don't use pipe-pane anymore since pi --mode json redirects directly to the log file.

	sess := &session.Session{
		ID:          id,
		Name:        runName,
		Model:       runModel,
		Task:        task,
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

	// Spawn detached watcher before printing ID so it's already running.
	if hasWatcherNotifications(notifyOptions) && !runWait {
		if err := spawnWatcher(id, notifyOptions); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not spawn watcher: %v\n", err)
		}
	}

	// ID goes to stdout so callers can capture it cleanly.
	fmt.Println(id)

	// Hints go to stderr so they don't pollute captured output.
	fmt.Fprintf(os.Stderr, "model:  %s\n", runModel)
	fmt.Fprintf(os.Stderr, "log:    %s\n", logFile)
	fmt.Fprintf(os.Stderr, "\nTo monitor:  agentctl monitor %s\n", id)
	fmt.Fprintf(os.Stderr, "To attach:   agentctl attach %s\n", id)
	if notifyOptions.PiSessionID != "" {
		fmt.Fprintf(os.Stderr, "Notify:      pi session %s\n", notifyOptions.PiSessionID)
	}
	if notifyOptions.EventDir != "" {
		fmt.Fprintf(os.Stderr, "Notify:      event dir %s (channel %s)\n", notifyOptions.EventDir, notifyOptions.EventChannel)
	}

	if runWait {
		fmt.Fprintln(os.Stderr, "\nWaiting for agent to finish...")
		for tmux.SessionExists(tmuxSession) {
			time.Sleep(500 * time.Millisecond)
		}
		// Cache turns+cost into session JSON so ls/status/costs don't rescan.
		if err := cacheSessionLogStats(sess); err != nil {
			fmt.Fprintf(os.Stderr, "warn: cache session stats: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "done.\n\n")

		// Print the agent's final response to stdout.
		if data, err := os.ReadFile(logFile); err == nil && len(data) > 0 {
			text := extractLastTurnText(data)
			if text != "" {
				fmt.Println(text)
			}
		}
	}

	return nil
}

func buildRunScript(cwd, taskFile, model, logFile, self string) string {
	// stderr goes to a separate file so it doesn't pollute the NDJSON log.
	// Pi emits terminal escape sequences (OSC notifications) on stderr
	// that can be very large and break log parsing.
	stderrFile := logFile + ".stderr"
	return fmt.Sprintf(`#!/bin/sh
set -e
cd %s
task=$(cat %s)
exec pi --mode json --model %s --no-session -p "$task" 2>%s | %s record %s
`, shellQuote(cwd), shellQuote(taskFile), shellQuote(model), shellQuote(stderrFile), shellQuote(self), shellQuote(logFile))
}

func resolveWatcherNotifyOptions(notifyMunin bool, getenv func(string) string) (watcherNotifyOptions, error) {
	hasExplicitNotifier := notifyMunin || runNotifySession != "" || runNotifyEventDir != "" || runNotifyEventChannel != "" || runNotifyEventThread != ""

	notifySession := runNotifySession
	if notifySession == "" && !hasExplicitNotifier {
		notifySession = getenv(piSessionIDEnv)
	}

	options := watcherNotifyOptions{
		PiSessionID:  notifySession,
		EventDir:     runNotifyEventDir,
		EventChannel: runNotifyEventChannel,
		EventThread:  runNotifyEventThread,
	}

	if notifyMunin {
		if options.EventDir == "" {
			options.EventDir = getenv(muninEventsDirEnv)
		}
		if options.EventChannel == "" {
			options.EventChannel = getenv(muninChannelIDEnv)
		}
		if options.EventThread == "" {
			options.EventThread = getenv(muninThreadTsEnv)
		}
	}

	if err := validateWatcherNotifyOptions(options); err != nil {
		return watcherNotifyOptions{}, err
	}
	if notifyMunin && (options.EventDir == "" || options.EventChannel == "") {
		return watcherNotifyOptions{}, fmt.Errorf(
			"--notify-munin requires %s and %s (or explicit --notify-event-dir/--notify-event-channel)",
			muninEventsDirEnv,
			muninChannelIDEnv,
		)
	}
	return options, nil
}

func validateWatcherNotifyOptions(options watcherNotifyOptions) error {
	hasEventDir := options.EventDir != ""
	hasEventChannel := options.EventChannel != ""
	hasEventThread := options.EventThread != ""

	if hasEventChannel && !hasEventDir {
		return fmt.Errorf("--notify-event-channel requires --notify-event-dir")
	}
	if hasEventThread && !hasEventDir {
		return fmt.Errorf("--notify-event-thread requires --notify-event-dir")
	}
	if hasEventDir && !hasEventChannel {
		return fmt.Errorf("--notify-event-dir requires --notify-event-channel")
	}

	return nil
}

func hasWatcherNotifications(options watcherNotifyOptions) bool {
	return options.PiSessionID != "" || options.EventDir != ""
}

// spawnWatcher starts a detached 'agentctl watch' process that notifies
// completion backends when the agent finishes. The process is fully detached
// (new process group, no stdin/stdout/stderr) so it survives after `agentctl run` exits.
func spawnWatcher(agentID string, options watcherNotifyOptions) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(self, watcherArgs(agentID, options)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // detach from parent process group
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func watcherArgs(agentID string, options watcherNotifyOptions) []string {
	args := []string{"watch", agentID}
	if options.PiSessionID != "" {
		args = append(args, "--notify-session", options.PiSessionID)
	}
	if options.EventDir != "" {
		args = append(args,
			"--notify-event-dir", options.EventDir,
			"--notify-event-channel", options.EventChannel,
		)
		if options.EventThread != "" {
			args = append(args, "--notify-event-thread", options.EventThread)
		}
	}
	if options.Progress {
		args = append(args, "--progress")
	}
	return args
}

func resolveRunTask(task string, taskFile string) (string, error) {
	hasTask := task != ""
	hasTaskFile := taskFile != ""
	if hasTask == hasTaskFile {
		return "", fmt.Errorf("exactly one of --task or --task-file must be provided")
	}

	if hasTask {
		return task, nil
	}

	taskBytes, err := os.ReadFile(taskFile)
	if err != nil {
		return "", fmt.Errorf("read --task-file %q: %w", taskFile, err)
	}
	return string(taskBytes), nil
}

func getenv(key string) string {
	return os.Getenv(key)
}

// shellQuote wraps s in single quotes, escaping any existing single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
