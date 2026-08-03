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

func TestCompletionSummaryUsesAssistantTextOnly(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Say hello"}]}}`,
		`{"type":"tool_execution_start","toolName":"read","args":{"path":"cmd/watch.go"}}`,
		`{"type":"tool_execution_end","toolName":"read","result":{"content":[{"type":"text","text":"package cmd"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hello there."}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello there."}`,
		`{"type":"turn_end","message":{"usage":{"totalTokens":123,"cost":{"total":0.001}}}}`,
	}, "\n"))

	if got := completionSummary(data); got != "Hello there." {
		t.Fatalf("expected assistant text, got %q", got)
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
		Text    string `json:"text"`
		Replace bool   `json:"replace"`
		Kind    string `json:"kind"`
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile returned error: %v", err)
		}
		var event struct {
			Text    string `json:"text"`
			Replace bool   `json:"replace"`
			Kind    string `json:"kind"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}
		last = event
	}

	if last.Text != "Thinking: Need to inspect the directory" {
		t.Fatalf("expected accumulated thinking text, got %q", last.Text)
	}
	if last.Replace {
		t.Fatal("expected thinking progress not to set legacy replace")
	}
	if last.Kind != "status" {
		t.Fatalf("expected thinking progress kind status, got %q", last.Kind)
	}
}

func TestCompletionSummaryPreservesAssistantBlockquoteLines(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"> quoted reply"}`,
		`{"type":"text_end","contentIndex":0,"content":"> quoted reply"}`,
	}, "\n"))

	got := completionSummary(data)
	if got != "> quoted reply" {
		t.Fatalf("expected assistant blockquote line to be preserved, got %q", got)
	}
}

func TestCompletionSummaryEmptyLog(t *testing.T) {
	got := completionSummary([]byte(""))
	if got != "" {
		t.Fatalf("expected empty for empty log, got %q", got)
	}
}

func TestCompletionSummarySkipsToolResults(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"tool_execution_end","toolName":"bash","result":{"content":[{"type":"text","text":"hello world"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"The output was hello."}`,
		`{"type":"text_end","contentIndex":0,"content":"The output was hello."}`,
	}, "\n"))

	got := completionSummary(data)
	if got != "The output was hello." {
		t.Fatalf("expected assistant text, got %q", got)
	}
}

func TestCompletionSummaryPrefersFinalAssistantTurn(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Earlier assistant turn"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Earlier assistant turn"}}`,
		`{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":100}}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Final assistant "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"answer"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Final assistant answer"}}`,
		`{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":200}}}`,
	}, "\n"))

	if got := completionSummary(data); got != "Final assistant answer" {
		t.Fatalf("expected final nested assistant turn, got %q", got)
	}
}

func TestCompletionSummaryUsesBoundedHeadAndTailWithVisibleMarker(t *testing.T) {
	bigText := "BEGIN-🙂" + strings.Repeat("界", completionSummaryMaxChars*2) + "🚀-END"
	data := []byte(strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		fmt.Sprintf(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":%q}}`, bigText),
		fmt.Sprintf(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":%q}}`, bigText),
		`{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":12345}}}`,
	}, "\n"))

	got := completionSummary(data)
	if len([]rune(got)) > completionSummaryMaxChars {
		t.Fatalf("expected at most %d chars, got %d", completionSummaryMaxChars, len([]rune(got)))
	}
	if !strings.HasPrefix(got, "BEGIN-🙂") || !strings.HasSuffix(got, "🚀-END") {
		t.Fatal("expected useful head and tail to be preserved")
	}
	if !strings.Contains(got, "completion summary truncated; beginning and end shown") {
		t.Fatalf("expected visible truncation marker, got %q", got)
	}
}

func TestCompletionSummaryPreservesPreviousTextForFinalToolOnlyTurn(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Previous non-empty final response"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Previous non-empty final response"}}`,
		`{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":100}}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"command\":\"go test ./...\"}"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1}}`,
		`{"type":"tool_execution_start","toolName":"bash","args":{"command":"go test ./..."}}`,
		`{"type":"tool_execution_end","toolName":"bash","result":{"content":[{"type":"text","text":"ok"}]},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash"}],"usage":{"totalTokens":200}}}`,
	}, "\n"))

	if got := completionSummary(data); got != "Previous non-empty final response" {
		t.Fatalf("expected previous non-empty response after tool-only final turn, got %q", got)
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
	if !strings.Contains(msg, "`agent_result` with ID `abc12345`") || !strings.Contains(msg, "`agentctl dump abc12345`") {
		t.Fatalf("expected both recovery hints, got: %q", msg)
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
