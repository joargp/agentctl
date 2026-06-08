package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/nxadm/tail"
)

func TestCompletionSummaryLinesUsesAssistantTextOnly(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Say hello"}]}}`,
		`{"type":"tool_execution_start","toolName":"read","args":{"path":"cmd/watch.go"}}`,
		`{"type":"tool_execution_end","toolName":"read","result":{"content":[{"type":"text","text":"package cmd"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hello there."}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello there."}`,
		`{"type":"turn_end","message":{"usage":{"totalTokens":123,"cost":{"total":0.001}}}}`,
	}, "\n"))

	got := completionSummaryLines(data)
	want := []string{"Hello there."}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected line %d = %q, got %q (all: %#v)", i, want[i], got[i], got)
		}
	}
}

func TestEmitProgressLineAccumulatesThinkingDeltas(t *testing.T) {
	dir := t.TempDir()
	s := &session.Session{ID: "agent-1", Model: "gpt-5.5", Task: "test task"}
	opts := watcherNotifyOptions{EventDir: dir, EventChannel: "C123"}
	turnCount := 0
	lastStatus := ""
	progressState := progressEventState{}

	emitProgressLine(&tail.Line{Text: `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"Need to inspect "}}`}, s, opts, &turnCount, &lastStatus, &progressState)
	emitProgressLine(&tail.Line{Text: `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"the directory"}}`}, s, opts, &turnCount, &lastStatus, &progressState)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 progress events, got %d", len(entries))
	}

	var last struct {
		Text string `json:"text"`
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile returned error: %v", err)
		}
		var event struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}
		last = event
	}

	if last.Text != "Thinking: Need to inspect the directory" {
		t.Fatalf("expected accumulated thinking text, got %q", last.Text)
	}
}

func TestCompletionSummaryLinesPreservesAssistantBlockquoteLines(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"> quoted reply"}`,
		`{"type":"text_end","contentIndex":0,"content":"> quoted reply"}`,
	}, "\n"))

	got := completionSummaryLines(data)
	if len(got) != 1 || got[0] != "> quoted reply" {
		t.Fatalf("expected assistant blockquote line to be preserved, got %#v", got)
	}
}

func TestCompletionSummaryLinesEmptyLog(t *testing.T) {
	got := completionSummaryLines([]byte(""))
	if len(got) != 0 {
		t.Fatalf("expected empty for empty log, got %v", got)
	}
}

func TestCompletionSummaryLinesSkipsToolResults(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"tool_execution_end","toolName":"bash","result":{"content":[{"type":"text","text":"hello world"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"The output was hello."}`,
		`{"type":"text_end","contentIndex":0,"content":"The output was hello."}`,
	}, "\n"))

	got := completionSummaryLines(data)
	// Should only include the assistant text, not the tool result
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d: %#v", len(got), got)
	}
	if got[0] != "The output was hello." {
		t.Fatalf("expected assistant text, got %q", got[0])
	}
}

func TestCompletionSummaryLinesTruncatesTo20Lines(t *testing.T) {
	var lines []string
	lines = append(lines, `{"type":"text_start","contentIndex":0}`)
	// Generate 25 lines of text
	bigText := ""
	for i := 0; i < 25; i++ {
		bigText += fmt.Sprintf("Line %d\n", i)
	}
	lines = append(lines, fmt.Sprintf(`{"type":"text_delta","contentIndex":0,"delta":%q}`, bigText))
	lines = append(lines, fmt.Sprintf(`{"type":"text_end","contentIndex":0,"content":%q}`, bigText))
	data := []byte(strings.Join(lines, "\n"))

	got := completionSummaryLines(data)
	if len(got) > 20 {
		t.Fatalf("expected max 20 lines, got %d", len(got))
	}
}

func TestTruncateTask(t *testing.T) {
	// Single line under limit
	if got := truncateTask("hello", 100); got != "hello" {
		t.Fatalf("expected unchanged short task, got %q", got)
	}
	// Multi-line takes first line only
	if got := truncateTask("line1\nline2\nline3", 100); got != "line1" {
		t.Fatalf("expected first line only, got %q", got)
	}
	// Long first line gets truncated
	long := strings.Repeat("x", 200)
	got := truncateTask(long, 100)
	if len(got) > 100 {
		t.Fatalf("expected truncated to 100, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ... suffix, got %q", got)
	}
	// Whitespace trimmed
	if got := truncateTask("  hello  ", 100); got != "hello" {
		t.Fatalf("expected trimmed, got %q", got)
	}
}

func TestCompletionMessageFallsBackToFullLogWhenTailMissesDelta(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "session.log")

	big := strings.Repeat("A", 600*1024)
	data := strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		fmt.Sprintf(`{"type":"text_delta","contentIndex":0,"delta":%q}`, big),
		fmt.Sprintf(`{"type":"text_end","contentIndex":0,"content":%q}`, big),
	}, "\n")
	if err := os.WriteFile(logFile, []byte(data), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	s := &session.Session{
		ID:      "abc12345",
		Model:   "openai/gpt-5.4",
		Task:    "Write a long response",
		LogFile: logFile,
	}

	msg := completionMessage(s)
	if !strings.Contains(msg, "**Summary:**") {
		t.Fatalf("expected completion message to include summary block, got: %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("A", 64)) {
		t.Fatalf("expected completion message to include assistant text from full-log fallback")
	}
}

func TestRunWatchSkipsNotificationsForCancelledSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logFile := filepath.Join(home, "session.log")
	if err := os.WriteFile(logFile, []byte(`{"type":"turn_end","message":{"usage":{"cost":{"total":0.01}}}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	s := &session.Session{
		ID:          "cancel123",
		Model:       "openai/gpt-5.4",
		Task:        "cancelled task",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     logFile,
		StartedAt:   time.Now(),
	}
	if err := session.Save(s); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := markSessionCancelled(s); err != nil {
		t.Fatalf("mark cancelled: %v", err)
	}

	eventDir := t.TempDir()
	prevSession := watchNotifySession
	prevEventDir := watchNotifyEventDir
	prevEventChannel := watchNotifyEventChannel
	prevEventThread := watchNotifyEventThread
	prevCommands := watchNotifyCommands
	prevProgress := watchProgress
	defer func() {
		watchNotifySession = prevSession
		watchNotifyEventDir = prevEventDir
		watchNotifyEventChannel = prevEventChannel
		watchNotifyEventThread = prevEventThread
		watchNotifyCommands = prevCommands
		watchProgress = prevProgress
	}()

	watchNotifySession = ""
	watchNotifyEventDir = eventDir
	watchNotifyEventChannel = "C123"
	watchNotifyEventThread = ""
	watchNotifyCommands = nil
	watchProgress = false

	if err := runWatch(nil, []string{s.ID}); err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}

	entries, err := os.ReadDir(eventDir)
	if err != nil {
		t.Fatalf("read event dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no completion event files for cancelled session, got %d", len(entries))
	}
	if sessionCancelled(s) {
		t.Fatal("expected cancel marker to be cleared after watch handles cancellation")
	}
}

func TestRunWatchSkipsCommandNotificationsForCancelledSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logFile := filepath.Join(home, "session.log")
	if err := os.WriteFile(logFile, []byte(`{"type":"turn_end","message":{"usage":{"cost":{"total":0.01}}}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	s := &session.Session{
		ID:          "cancelcmd",
		Model:       "openai/gpt-5.4",
		Task:        "cancelled task",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     logFile,
		StartedAt:   time.Now(),
	}
	if err := session.Save(s); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := markSessionCancelled(s); err != nil {
		t.Fatalf("mark cancelled: %v", err)
	}

	outPath := filepath.Join(home, "notified.json")
	command := writeWatchNotifierScript(t, home, "notify.sh", `#!/bin/sh
cat > "$AGENTCTL_NOTIFY_OUT"
`)
	t.Setenv("AGENTCTL_NOTIFY_OUT", outPath)

	prevSession := watchNotifySession
	prevEventDir := watchNotifyEventDir
	prevEventChannel := watchNotifyEventChannel
	prevEventThread := watchNotifyEventThread
	prevCommands := watchNotifyCommands
	prevProgress := watchProgress
	defer func() {
		watchNotifySession = prevSession
		watchNotifyEventDir = prevEventDir
		watchNotifyEventChannel = prevEventChannel
		watchNotifyEventThread = prevEventThread
		watchNotifyCommands = prevCommands
		watchProgress = prevProgress
	}()

	watchNotifySession = ""
	watchNotifyEventDir = ""
	watchNotifyEventChannel = ""
	watchNotifyEventThread = ""
	watchNotifyCommands = []string{command}
	watchProgress = false

	if err := runWatch(nil, []string{s.ID}); err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected command notifier to be skipped, stat err=%v", err)
	}
}

func TestRunWatchInvokesCommandAndEventNotifications(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logFile := filepath.Join(home, "session.log")
	logData := strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Finished cleanly."}`,
		`{"type":"text_end","contentIndex":0,"content":"Finished cleanly."}`,
		`{"type":"turn_end","message":{"usage":{"cost":{"total":0.02}}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(logFile, []byte(logData), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	startedAt := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	s := &session.Session{
		ID:          "donecmd1",
		Name:        "reviewer",
		Model:       "claude-opus-4-6",
		Task:        "review the diff",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     logFile,
		StartedAt:   startedAt,
	}
	if err := session.Save(s); err != nil {
		t.Fatalf("save session: %v", err)
	}

	commandOutPath := filepath.Join(home, "command-payload.json")
	command := writeWatchNotifierScript(t, home, "notify.sh", `#!/bin/sh
cat > "$AGENTCTL_NOTIFY_OUT"
`)
	t.Setenv("AGENTCTL_NOTIFY_OUT", commandOutPath)
	eventDir := filepath.Join(home, "events")

	prevSession := watchNotifySession
	prevEventDir := watchNotifyEventDir
	prevEventChannel := watchNotifyEventChannel
	prevEventThread := watchNotifyEventThread
	prevCommands := watchNotifyCommands
	prevProgress := watchProgress
	defer func() {
		watchNotifySession = prevSession
		watchNotifyEventDir = prevEventDir
		watchNotifyEventChannel = prevEventChannel
		watchNotifyEventThread = prevEventThread
		watchNotifyCommands = prevCommands
		watchProgress = prevProgress
	}()

	watchNotifySession = ""
	watchNotifyEventDir = eventDir
	watchNotifyEventChannel = "C123"
	watchNotifyEventThread = ""
	watchNotifyCommands = []string{command}
	watchProgress = false

	if err := runWatch(nil, []string{s.ID}); err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}

	commandData, err := os.ReadFile(commandOutPath)
	if err != nil {
		t.Fatalf("expected command notifier payload: %v", err)
	}
	var payload struct {
		SchemaVersion int    `json:"schemaVersion"`
		Event         string `json:"event"`
		Session       struct {
			ID        string    `json:"id"`
			Name      string    `json:"name"`
			Model     string    `json:"model"`
			Task      string    `json:"task"`
			Cwd       string    `json:"cwd"`
			StartedAt time.Time `json:"startedAt"`
			LogFile   string    `json:"logFile"`
			Turns     int       `json:"turns"`
			TotalCost float64   `json:"totalCost"`
		} `json:"session"`
		Message     string `json:"message"`
		DumpCommand string `json:"dumpCommand"`
	}
	if err := json.Unmarshal(commandData, &payload); err != nil {
		t.Fatalf("Unmarshal command payload returned error: %v", err)
	}
	if payload.SchemaVersion != 1 || payload.Event != "session.completed" {
		t.Fatalf("unexpected command payload header: %+v", payload)
	}
	if payload.Session.ID != s.ID || payload.Session.Name != "reviewer" || payload.Session.Turns != 1 || payload.Session.TotalCost != 0.02 {
		t.Fatalf("unexpected session payload: %+v", payload.Session)
	}
	if !strings.Contains(payload.Message, "Finished cleanly.") {
		t.Fatalf("expected completion message in command payload, got %q", payload.Message)
	}
	if payload.DumpCommand != "agentctl dump donecmd1" {
		t.Fatalf("unexpected dump command %q", payload.DumpCommand)
	}

	entries, err := os.ReadDir(eventDir)
	if err != nil {
		t.Fatalf("read event dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one event file, got %d", len(entries))
	}
}

func writeWatchNotifierScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) returned error: %v", name, err)
	}
	return path
}
