package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWatcherNotifyOptionsNotifyMuninUsesEnv(t *testing.T) {
	prevSession := runNotifySession
	prevEventDir := runNotifyEventDir
	prevEventChannel := runNotifyEventChannel
	prevEventThread := runNotifyEventThread
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""

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
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
	}()

	runNotifySession = ""
	runNotifyEventDir = "/custom/events"
	runNotifyEventChannel = "C999"
	runNotifyEventThread = "222.333"

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
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""

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
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
	}()

	runNotifySession = ""
	runNotifyEventDir = ""
	runNotifyEventChannel = ""
	runNotifyEventThread = ""

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
	defer func() {
		runNotifySession = prevSession
		runNotifyEventDir = prevEventDir
		runNotifyEventChannel = prevEventChannel
		runNotifyEventThread = prevEventThread
	}()

	runNotifySession = ""
	runNotifyEventDir = "/workspace/events"
	runNotifyEventChannel = "C123"
	runNotifyEventThread = ""

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

func TestBuildRunScriptUsesRecorder(t *testing.T) {
	script := buildRunScript("/tmp/work", "/tmp/task.txt", "gpt-5.4", "/tmp/run.log", "/usr/local/bin/agentctl")

	if strings.Contains(script, "tee -a") {
		t.Fatalf("expected run script to stop using tee, got %q", script)
	}
	if !strings.Contains(script, "'/usr/local/bin/agentctl' record '/tmp/run.log'") {
		t.Fatalf("expected run script to invoke recorder, got %q", script)
	}
	if !strings.Contains(script, "2>'/tmp/run.log.stderr'") {
		t.Fatalf("expected stderr redirect to separate file, got %q", script)
	}
	if strings.Contains(script, "2>&1") {
		t.Fatalf("expected no stderr-to-stdout merge, got %q", script)
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
