package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/notify"
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
  agentctl run --model gpt-5.4 --task "review this PR" --cwd /repos/myapp --wait
  agentctl run --model claude-opus-4-6 --thinking high --task "refactor the session store"`,
	RunE: runRun,
}

type watcherNotifyOptions struct {
	PiSessionID  string
	EventDir     string
	EventChannel string
	EventThread  string
	Progress     bool
	Commands     []string
}

var thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

var (
	runModel              string
	runThinking           string
	runTask               string
	runTaskFile           string
	runCwd                string
	runName               string
	runWait               bool
	runRender             bool
	runNotifySession      string
	runNotifyMunin        bool
	runNotifyEventDir     string
	runNotifyEventChannel string
	runNotifyEventThread  string
	runNotifyCommands     []string
	runStartupTimeout     time.Duration
)

func init() {
	runCmd.Flags().StringVar(&runModel, "model", "", "model to pass to pi (required)")
	runCmd.Flags().StringVar(&runThinking, "thinking", "", "thinking level to pass to pi: "+strings.Join(thinkingLevels, ", "))
	runCmd.Flags().StringVar(&runTask, "task", "", "task prompt (mutually exclusive with --task-file)")
	runCmd.Flags().StringVar(&runTaskFile, "task-file", "", "path to file containing task prompt (mutually exclusive with --task)")
	runCmd.Flags().StringVar(&runCwd, "cwd", "", "working directory (default: current dir)")
	runCmd.Flags().StringVar(&runName, "name", "", "short label for monitor output (default: model name)")
	runCmd.Flags().BoolVar(&runWait, "wait", false, "block until the agent session finishes")
	runCmd.Flags().BoolVar(&runRender, "render", false, "show human-readable output in the tmux session instead of raw JSON")
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
	runCmd.Flags().StringArrayVar(&runNotifyCommands, "notify-command", nil,
		"executable path to invoke with completion JSON on stdin when the agent finishes (repeatable)")
	runCmd.Flags().DurationVar(&runStartupTimeout, "startup-timeout", 60*time.Second,
		"wait for provider-backed output before reporting the session as started")
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

	if err := validateThinkingLevel(runThinking); err != nil {
		return err
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
	runtimeFile := filepath.Join(dataDir, "runtime", id+".json")
	tmuxSession := "agentctl-" + id

	// Write task to a file so the supervisor can read it safely,
	// avoiding any quoting issues with the task content.
	if err := os.WriteFile(taskFile, []byte(task), 0o644); err != nil {
		return fmt.Errorf("write task file: %w", err)
	}

	self, err := resolveExecutable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	sess := &session.Session{
		ID:          id,
		Name:        runName,
		Model:       runModel,
		Thinking:    runThinking,
		Task:        task,
		Cwd:         cwd,
		TmuxSession: tmuxSession,
		LogFile:     logFile,
		ScriptFile:  scriptFile,
		TaskFile:    taskFile,
		RuntimeFile: runtimeFile,
		StartedAt:   time.Now(),
	}
	if err := session.Save(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	cleanupStartFailure := func() {
		_ = session.Remove(id)
		_ = os.Remove(scriptFile)
		_ = os.Remove(taskFile)
		_ = removeRuntimeState(runtimeFile)
	}

	// Wrapper script: exec the hidden supervisor command directly. The
	// supervisor keeps pi in its own process group, tears down the full child
	// tree on every exit path, and records the runtime PID/PGID for external
	// cleanup during kill/watch flows.
	script := buildRunScript(id, self, runRender)
	if err := os.WriteFile(scriptFile, []byte(script), 0o755); err != nil {
		cleanupStartFailure()
		return fmt.Errorf("write script: %w", err)
	}

	// Ensure log file exists before pi starts writing to it.
	if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}

	if err := tmux.EnsureSocket(); err != nil {
		cleanupStartFailure()
		return err
	}
	// Start the session directly running the script — no outer shell.
	// When the supervisor exits the window closes and the tmux session disappears.
	if err := tmux.NewSession(tmuxSession, "sh", scriptFile); err != nil {
		cleanupStartFailure()
		return err
	}

	if err := waitForStartupReady(sess, runStartupTimeout); err != nil {
		if cleanupErr := cleanupSessionProcessTree(sess, runtimeCleanupGrace); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "warn: clean up failed startup: %v\n", cleanupErr)
		}
		return err
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
	for _, command := range notifyOptions.Commands {
		fmt.Fprintf(os.Stderr, "Notify:      command %s\n", command)
	}

	if runWait {
		fmt.Fprintln(os.Stderr, "\nWaiting for agent to finish...")
		for tmux.SessionExists(tmuxSession) {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cleanupSessionProcessTree(sess, runtimeCleanupGrace); err != nil {
			fmt.Fprintf(os.Stderr, "warn: clean up process tree: %v\n", err)
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

func resolveExecutable() (string, error) {
	return resolveExecutableWith(os.Executable, os.Args, exec.LookPath, filepath.Abs, filepath.EvalSymlinks)
}

func resolveExecutableWith(
	osExecutable func() (string, error),
	args []string,
	lookPath func(string) (string, error),
	abs func(string) (string, error),
	evalSymlinks func(string) (string, error),
) (string, error) {
	exe, err := osExecutable()
	if err == nil && !looksLikeDynamicLinker(exe) {
		return exe, nil
	}

	if len(args) == 0 || args[0] == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("executable resolved to dynamic linker: %s", exe)
	}

	path := args[0]
	if !filepath.IsAbs(path) {
		path, err = lookPath(path)
		if err != nil {
			return "", err
		}
	}

	path, err = abs(path)
	if err != nil {
		return "", err
	}
	path, err = evalSymlinks(path)
	if err != nil {
		return "", err
	}
	return path, nil
}

func looksLikeDynamicLinker(path string) bool {
	path = strings.ToLower(path)
	return strings.Contains(path, "ld-musl") || strings.Contains(path, "ld-linux")
}

func buildRunScript(id, self string, render bool) string {
	renderFlag := ""
	if render {
		renderFlag = " --render"
	}
	return fmt.Sprintf(`#!/bin/sh
set -e
exec %s supervise%s %s
`, shellQuote(self), renderFlag, shellQuote(id))
}

type startupState int

const (
	startupPending startupState = iota
	startupReady
	startupFailed
)

func waitForStartupReady(s *session.Session, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("startup timeout must be positive")
	}

	deadline := time.Now().Add(timeout)
	var offset int64
	var lastFailure string

	for {
		state, detail, nextOffset := scanStartupLog(s.LogFile, offset)
		offset = nextOffset
		switch state {
		case startupReady:
			return nil
		case startupFailed:
			if detail != "" {
				lastFailure = detail
			}
			return startupFailureError(s, lastFailure)
		}

		if !tmux.SessionExists(s.TmuxSession) {
			state, detail, _ = scanStartupLog(s.LogFile, offset)
			if state == startupReady {
				return nil
			}
			if detail != "" {
				lastFailure = detail
			}
			return startupFailureError(s, lastFailure)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("agent %s did not produce provider-backed output within %s (log: %s)", s.ID, timeout, s.LogFile)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func scanStartupLog(path string, offset int64) (startupState, string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return startupPending, "", offset
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return startupPending, "", offset
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	currentOffset := offset
	for scanner.Scan() {
		line := scanner.Bytes()
		currentOffset += int64(len(line)) + 1
		state, detail := startupEventStatus(line)
		if state != startupPending {
			return state, detail, currentOffset
		}
	}

	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		currentOffset = pos
	}
	return startupPending, "", currentOffset
}

func startupEventStatus(line []byte) (startupState, string) {
	var event map[string]interface{}
	if err := json.Unmarshal(line, &event); err != nil {
		return startupPending, ""
	}

	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_start":
		msg, _ := event["message"].(map[string]interface{})
		if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
			return startupFailed, apiErrorText(msg)
		}
		if role, _ := msg["role"].(string); role == "assistant" {
			return startupReady, ""
		}
	case "message_update":
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae == nil {
			return startupPending, ""
		}
		switch aeType, _ := ae["type"].(string); aeType {
		case "thinking_start", "thinking_delta", "text_start", "text_delta":
			return startupReady, ""
		}
	case "thinking_start", "thinking_delta", "text_start", "text_delta", "tool_execution_start":
		return startupReady, ""
	case "turn_end":
		msg, _ := event["message"].(map[string]interface{})
		if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
			return startupFailed, apiErrorText(msg)
		}
		if _, _, ok := usageSummary(msg); ok {
			return startupReady, ""
		}
	}

	return startupPending, ""
}

func startupFailureError(s *session.Session, detail string) error {
	if detail == "" {
		detail = stderrTail(s.LogFile + ".stderr")
	}
	if detail == "" {
		detail = "pi exited before producing provider-backed output"
	}
	return fmt.Errorf("agent %s failed to start with model %s: %s (log: %s)", s.ID, s.Model, detail, s.LogFile)
}

func stderrTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return truncateRunesASCII(strings.Join(lines, "\n"), 1000)
}

func resolveWatcherNotifyOptions(notifyMunin bool, getenv func(string) string) (watcherNotifyOptions, error) {
	hasExplicitNotifier := notifyMunin || runNotifySession != "" || runNotifyEventDir != "" || runNotifyEventChannel != "" || runNotifyEventThread != "" || len(runNotifyCommands) > 0

	notifySession := runNotifySession
	if notifySession == "" && !hasExplicitNotifier {
		notifySession = getenv(piSessionIDEnv)
	}

	options := watcherNotifyOptions{
		PiSessionID:  notifySession,
		EventDir:     runNotifyEventDir,
		EventChannel: runNotifyEventChannel,
		EventThread:  runNotifyEventThread,
		Commands:     append([]string(nil), runNotifyCommands...),
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
	for _, command := range options.Commands {
		if err := notify.ValidateCompletionCommand(command); err != nil {
			return err
		}
	}

	return nil
}

func hasWatcherNotifications(options watcherNotifyOptions) bool {
	return options.PiSessionID != "" || options.EventDir != "" || len(options.Commands) > 0
}

// spawnWatcher starts a detached 'agentctl watch' process that notifies
// completion backends when the agent finishes. The process is fully detached
// (new process group, no stdin/stdout/stderr) so it survives after `agentctl run` exits.
func spawnWatcher(agentID string, options watcherNotifyOptions) error {
	self, err := resolveExecutable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	logFile, err := watcherLogFile(agentID)
	if err != nil {
		return fmt.Errorf("resolve watcher log file: %w", err)
	}
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open watcher log file: %w", err)
	}
	defer log.Close()

	cmd := exec.Command(self, watcherArgs(agentID, options)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // detach from parent process group
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd.Start()
}

func watcherLogFile(agentID string) (string, error) {
	dataDir, err := session.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "logs", agentID+".watch.log"), nil
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
	for _, command := range options.Commands {
		args = append(args, "--notify-command", command)
	}
	return args
}

func validateThinkingLevel(level string) error {
	if level == "" {
		return nil
	}
	for _, valid := range thinkingLevels {
		if level == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid --thinking level %q (valid: %s)", level, strings.Join(thinkingLevels, ", "))
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
