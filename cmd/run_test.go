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
