package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joargp/agentctl/internal/session"
)

func TestResolveWatcherNotifyOptionsNotifyMuninUsesEnv(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""
	runNotifyCommands = nil

	env := map[string]string{
		piSessionIDEnv:    "session-123",
		muninEventsDirEnv: "/workspace/events",
		muninChannelIDEnv: "C123",
		muninThreadTsEnv:  "1710000000.000100",
	}

	options, err := resolveWatcherNotifyOptions(true, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("resolveWatcherNotifyOptions returned error: %v", err)
	}

	if options.PiSessionID != "" {
		t.Fatalf("expected explicit notify-munin to suppress PI_SESSION_ID fallback, got %q", options.PiSessionID)
	}
	if options.EventDir != "/workspace/events" {
		t.Fatalf("expected event dir from env, got %q", options.EventDir)
	}
	if options.EventChannel != "C123" {
		t.Fatalf("expected event channel from env, got %q", options.EventChannel)
	}
	if options.EventThread != "1710000000.000100" {
		t.Fatalf("expected event thread from env, got %q", options.EventThread)
	}
}

func TestResolveWatcherNotifyOptionsNotifyMuninExplicitFlagsOverrideEnv(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = "/custom/events"
	runNotifyEventChannel = "C999"
	runNotifyEventThread = "222.333"
	runNotifyCommands = nil

	env := map[string]string{
		muninEventsDirEnv: "/workspace/events",
		muninChannelIDEnv: "C123",
		muninThreadTsEnv:  "1710000000.000100",
	}

	options, err := resolveWatcherNotifyOptions(true, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("resolveWatcherNotifyOptions returned error: %v", err)
	}

	if options.EventDir != "/custom/events" || options.EventChannel != "C999" || options.EventThread != "222.333" {
		t.Fatalf("expected explicit flags to override env, got %+v", options)
	}
}

func TestResolveWatcherNotifyOptionsNotifyMuninRequiresContext(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""
	runNotifyCommands = nil

	_, err := resolveWatcherNotifyOptions(true, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error when notify-munin has no event context")
	}
}

func TestResolveWatcherNotifyOptionsFallsBackToPiSessionOnlyWhenNoExplicitNotifier(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""
	runNotifyCommands = nil

	options, err := resolveWatcherNotifyOptions(false, func(key string) string {
		if key == piSessionIDEnv {
			return "session-123"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("resolveWatcherNotifyOptions returned error: %v", err)
	}
	if options.PiSessionID != "session-123" {
		t.Fatalf("expected PI_SESSION_ID fallback when no explicit notifier is set, got %q", options.PiSessionID)
	}
}

func TestResolveWatcherNotifyOptionsExplicitEventNotifierSuppressesPiSessionFallback(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = "/workspace/events"
	runNotifyEventChannel = "C123"
	runNotifyEventThread = ""
	runNotifyCommands = nil

	options, err := resolveWatcherNotifyOptions(false, func(key string) string {
		if key == piSessionIDEnv {
			return "session-123"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("resolveWatcherNotifyOptions returned error: %v", err)
	}
	if options.PiSessionID != "" {
		t.Fatalf("expected explicit event notifier to suppress PI_SESSION_ID fallback, got %q", options.PiSessionID)
	}
}

func TestResolveWatcherNotifyOptionsNotifyCommandSuppressesPiSessionFallback(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""
	runNotifyCommands = []string{"./notify-test"}

	options, err := resolveWatcherNotifyOptions(false, func(key string) string {
		if key == piSessionIDEnv {
			return "session-123"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("resolveWatcherNotifyOptions returned error: %v", err)
	}
	if options.PiSessionID != "" {
		t.Fatalf("expected notify command to suppress PI_SESSION_ID fallback, got %q", options.PiSessionID)
	}
	if len(options.Commands) != 1 || options.Commands[0] != "./notify-test" {
		t.Fatalf("expected notify command to round-trip, got %#v", options.Commands)
	}
}

func TestResolveWatcherNotifyOptionsRejectsInvalidNotifyCommand(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	prevCommands := runNotifyCommands
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
		runNotifyCommands = prevCommands
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""
	runNotifyCommands = []string{"notify-test"}

	_, err := resolveWatcherNotifyOptions(false, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected invalid notify command to be rejected")
	}
	if !strings.Contains(err.Error(), "explicit executable path") {
		t.Fatalf("expected explicit path error, got %v", err)
	}
}

func TestWatcherArgsIncludesProgressFlag(t *testing.T) {
	args := watcherArgs("abc123", watcherNotifyOptions{
		EventDir:     "/workspace/events",
		EventChannel: "C123",
		EventThread:  "1710000000.000100",
		Progress:     true,
	})

	expected := []string{
		"watch", "abc123",
		"--notify-event-dir", "/workspace/events",
		"--notify-event-channel", "C123",
		"--notify-event-thread", "1710000000.000100",
		"--progress",
	}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("expected args[%d] = %q, got %q (all args: %v)", i, expected[i], args[i], args)
		}
	}
}

func TestWatcherArgsIncludesNotifyCommands(t *testing.T) {
	args := watcherArgs("abc123", watcherNotifyOptions{
		Commands: []string{"./notify-a", "/tmp/notify-b"},
	})

	expected := []string{
		"watch", "abc123",
		"--notify-command", "./notify-a",
		"--notify-command", "/tmp/notify-b",
	}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("expected args[%d] = %q, got %q (all args: %v)", i, expected[i], args[i], args)
		}
	}
}

func TestResolveExecutableUsesOSExecutableWhenNotLinker(t *testing.T) {
	lookedUp := false
	absoluted := false
	resolvedSymlink := false

	got, err := resolveExecutableWith(
		func() (string, error) { return "/usr/local/bin/agentctl", nil },
		[]string{"agentctl"},
		func(path string) (string, error) {
			lookedUp = true
			return path, nil
		},
		func(path string) (string, error) {
			absoluted = true
			return path, nil
		},
		func(path string) (string, error) {
			resolvedSymlink = true
			return path, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveExecutableWith returned error: %v", err)
	}
	if got != "/usr/local/bin/agentctl" {
		t.Fatalf("expected os.Executable path, got %q", got)
	}
	if lookedUp || absoluted || resolvedSymlink {
		t.Fatalf("expected no fallback helpers to run, got lookedUp=%v absoluted=%v resolvedSymlink=%v", lookedUp, absoluted, resolvedSymlink)
	}
}

func TestResolveExecutableFallsBackFromMuslLinkerToArgv0(t *testing.T) {
	lookedUp := false
	absoluted := false
	resolvedSymlink := false

	got, err := resolveExecutableWith(
		func() (string, error) { return "/lib/ld-musl-x86_64.so.1", nil },
		[]string{"agentctl"},
		func(path string) (string, error) {
			lookedUp = true
			if path != "agentctl" {
				t.Fatalf("expected LookPath input %q, got %q", "agentctl", path)
			}
			return "/workspace/bin/agentctl", nil
		},
		func(path string) (string, error) {
			absoluted = true
			if path != "/workspace/bin/agentctl" {
				t.Fatalf("expected Abs input %q, got %q", "/workspace/bin/agentctl", path)
			}
			return path, nil
		},
		func(path string) (string, error) {
			resolvedSymlink = true
			if path != "/workspace/bin/agentctl" {
				t.Fatalf("expected EvalSymlinks input %q, got %q", "/workspace/bin/agentctl", path)
			}
			return "/workspace/real/agentctl", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveExecutableWith returned error: %v", err)
	}
	if got != "/workspace/real/agentctl" {
		t.Fatalf("expected fallback path, got %q", got)
	}
	if !lookedUp || !absoluted || !resolvedSymlink {
		t.Fatalf("expected fallback helpers to run, got lookedUp=%v absoluted=%v resolvedSymlink=%v", lookedUp, absoluted, resolvedSymlink)
	}
}

func TestBuildRunScriptUsesRecorder(t *testing.T) {
	script := buildRunScript("abc123", "/usr/local/bin/agentctl", false)

	if !strings.Contains(script, "exec '/usr/local/bin/agentctl' supervise 'abc123'") {
		t.Fatalf("expected run script to exec supervisor, got %q", script)
	}
	if strings.Contains(script, "record") || strings.Contains(script, "pi --mode json") {
		t.Fatalf("expected run script to delegate to supervisor instead of piping directly, got %q", script)
	}
}

func TestBuildRunScriptWithRender(t *testing.T) {
	script := buildRunScript("abc123", "/usr/local/bin/agentctl", true)

	if !strings.Contains(script, "supervise --render 'abc123'") {
		t.Fatalf("expected --render flag in supervisor invocation, got %q", script)
	}
}

func TestValidateThinkingLevel(t *testing.T) {
	for _, level := range append([]string{""}, thinkingLevels...) {
		if err := validateThinkingLevel(level); err != nil {
			t.Fatalf("expected %q to be valid, got error: %v", level, err)
		}
	}

	err := validateThinkingLevel("ultra")
	if err == nil {
		t.Fatal("expected invalid thinking level to be rejected")
	}
	if !strings.Contains(err.Error(), "ultra") {
		t.Fatalf("expected error to mention rejected level, got %v", err)
	}
}

func TestResolveRunTaskWithInlineTask(t *testing.T) {
	task, err := resolveRunTask("do work", "")
	if err != nil {
		t.Fatalf("resolveRunTask returned error: %v", err)
	}
	if task != "do work" {
		t.Fatalf("expected inline task, got %q", task)
	}
}

func TestResolveRunTaskWithTaskFile(t *testing.T) {
	tmpDir := t.TempDir()
	taskPath := filepath.Join(tmpDir, "task.txt")
	if err := os.WriteFile(taskPath, []byte("file task"), 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}

	task, err := resolveRunTask("", taskPath)
	if err != nil {
		t.Fatalf("resolveRunTask returned error: %v", err)
	}
	if task != "file task" {
		t.Fatalf("expected task file content, got %q", task)
	}
}

func TestResolveRunTaskRejectsBothOrNeither(t *testing.T) {
	if _, err := resolveRunTask("", ""); err == nil {
		t.Fatal("expected error when neither --task nor --task-file is provided")
	}
	if _, err := resolveRunTask("inline", "/tmp/task.txt"); err == nil {
		t.Fatal("expected error when both --task and --task-file are provided")
	}
}

func TestStartupEventStatusReadyEvents(t *testing.T) {
	cases := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"checking"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`,
		`{"type":"thinking_start"}`,
		`{"type":"thinking_delta","delta":"checking"}`,
		`{"type":"text_start"}`,
		`{"type":"text_delta","delta":"hello"}`,
		`{"type":"tool_execution_start","toolName":"bash"}`,
		`{"type":"message_start","message":{"role":"assistant"}}`,
		`{"type":"turn_end","message":{"usage":{"totalTokens":12}}}`,
	}

	for _, input := range cases {
		state, detail := startupEventStatus([]byte(input))
		if state != startupReady {
			t.Fatalf("expected ready for %s, got state %d detail %q", input, state, detail)
		}
	}
}

func TestStartupEventStatusIgnoresUserMessageStart(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`
	state, detail := startupEventStatus([]byte(input))
	if state != startupPending || detail != "" {
		t.Fatalf("expected pending user message, got state %d detail %q", state, detail)
	}
}

func TestStartupEventStatusDetectsAPIError(t *testing.T) {
	input := `{"type":"message_start","message":{"stopReason":"error","errorMessage":"{\"error\":{\"message\":\"model google/not-real does not exist\"}}"}}`
	state, detail := startupEventStatus([]byte(input))
	if state != startupFailed {
		t.Fatalf("expected failed startup, got state %d detail %q", state, detail)
	}
	if !strings.Contains(detail, "model google/not-real does not exist") {
		t.Fatalf("expected provider error detail, got %q", detail)
	}
}

func TestScanStartupLogReturnsFirstTerminalState(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "agent.log")
	data := strings.Join([]string{
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hi"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(logFile, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	state, detail, offset := scanStartupLog(logFile, 0)
	if state != startupReady || detail != "" {
		t.Fatalf("expected ready without detail, got state %d detail %q", state, detail)
	}
	if offset <= 0 {
		t.Fatalf("expected scan offset to advance, got %d", offset)
	}
}

func TestStartupFailureErrorFallsBackToStderr(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(logFile+".stderr", []byte("provider rejected model\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := startupFailureError(&session.Session{
		ID:      "abc123",
		Model:   "google/not-real",
		LogFile: logFile,
	}, "")
	if err == nil {
		t.Fatal("expected startup failure error")
	}
	text := err.Error()
	for _, expected := range []string{"abc123", "google/not-real", "provider rejected model", logFile} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in error %q", expected, text)
		}
	}
}
