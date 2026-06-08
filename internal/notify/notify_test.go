package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteImmediateEvent(t *testing.T) {
	dir := t.TempDir()
	err := WriteImmediateEvent(dir, ImmediateEvent{
		ChannelID: "C123",
		Text:      "[AGENTCTL_DONE]\nAgent finished.",
		ThreadTs:  "1710000000.000100",
		Metadata: map[string]string{
			"source": "agentctl",
			"id":     "abc123",
		},
	})
	if err != nil {
		t.Fatalf("WriteImmediateEvent returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 event file, got %d", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("expected json event file, got %s", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var payload struct {
		Type      string            `json:"type"`
		ChannelID string            `json:"channelId"`
		Text      string            `json:"text"`
		ThreadTs  string            `json:"threadTs"`
		Metadata  map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if payload.Type != "immediate" {
		t.Fatalf("expected type immediate, got %s", payload.Type)
	}
	if payload.ChannelID != "C123" {
		t.Fatalf("expected channel C123, got %s", payload.ChannelID)
	}
	if payload.ThreadTs != "1710000000.000100" {
		t.Fatalf("expected thread ts to round-trip, got %s", payload.ThreadTs)
	}
	if payload.Metadata["id"] != "abc123" {
		t.Fatalf("expected metadata id abc123, got %s", payload.Metadata["id"])
	}
}

func TestWriteImmediateEventRequiresFields(t *testing.T) {
	if err := WriteImmediateEvent("", ImmediateEvent{ChannelID: "C123", Text: "hello"}); err == nil {
		t.Fatal("expected error for missing dir")
	}
	if err := WriteImmediateEvent(t.TempDir(), ImmediateEvent{Text: "hello"}); err == nil {
		t.Fatal("expected error for missing channel")
	}
	if err := WriteImmediateEvent(t.TempDir(), ImmediateEvent{ChannelID: "C123"}); err == nil {
		t.Fatal("expected error for missing text")
	}
}

func TestWriteProgressEvent(t *testing.T) {
	dir := t.TempDir()
	err := WriteProgressEvent(dir, ProgressEvent{
		ChannelID:  "C123",
		ThreadTs:   "1710000000.000100",
		SubagentID: "abc123",
		Text:       "All done",
		Replace:    true,
	})
	if err != nil {
		t.Fatalf("WriteProgressEvent returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 event file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "progress-abc123-") {
		t.Fatalf("expected filename to include subagent id, got %s", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var payload struct {
		Type       string `json:"type"`
		ChannelID  string `json:"channelId"`
		ThreadTs   string `json:"threadTs"`
		SubagentID string `json:"subagentId"`
		Text       string `json:"text"`
		Replace    bool   `json:"replace"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if payload.Type != "progress" {
		t.Fatalf("expected type progress, got %s", payload.Type)
	}
	if payload.SubagentID != "abc123" {
		t.Fatalf("expected subagent id abc123, got %s", payload.SubagentID)
	}
	if payload.Text != "All done" {
		t.Fatalf("expected progress text to round-trip, got %q", payload.Text)
	}
	if !payload.Replace {
		t.Fatal("expected replace flag to round-trip")
	}
}

func TestCleanupProgressFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"progress-abc123-1-1.json",
		"progress-abc123-2-2.json",
		"progress-other-3-3.json",
		"agentctl-done-4-4.json",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) returned error: %v", name, err)
		}
	}

	if err := CleanupProgressFiles(dir, "abc123"); err != nil {
		t.Fatalf("CleanupProgressFiles returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	remaining := make(map[string]bool, len(entries))
	for _, entry := range entries {
		remaining[entry.Name()] = true
	}
	if remaining["progress-abc123-1-1.json"] || remaining["progress-abc123-2-2.json"] {
		t.Fatalf("expected matching progress files to be removed, remaining=%v", remaining)
	}
	if !remaining["progress-other-3-3.json"] {
		t.Fatalf("expected other subagent progress file to remain, remaining=%v", remaining)
	}
	if !remaining["agentctl-done-4-4.json"] {
		t.Fatalf("expected done event to remain, remaining=%v", remaining)
	}
}

func TestSendCompletionCommandWritesPayloadToStdin(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "payload.json")
	command := writeNotifierScript(t, dir, "capture.sh", `#!/bin/sh
cat > "$AGENTCTL_NOTIFY_OUT"
`)
	t.Setenv("AGENTCTL_NOTIFY_OUT", outPath)

	payload := CompletionCommandPayload{
		SchemaVersion: 1,
		Event:         "session.completed",
		Session: CompletionCommandSession{
			ID:        "abc12345",
			Model:     "claude-opus-4-6",
			Task:      "original task",
			Cwd:       "/repo/path",
			StartedAt: time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
			LogFile:   "/tmp/abc12345.log",
			Turns:     3,
			TotalCost: 0.03,
		},
		Message:     "Agent finished.",
		DumpCommand: "agentctl dump abc12345",
	}
	if err := SendCompletionCommand(command, payload); err != nil {
		t.Fatalf("SendCompletionCommand returned error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var got CompletionCommandPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got.SchemaVersion != 1 || got.Event != "session.completed" {
		t.Fatalf("unexpected payload header: %+v", got)
	}
	if got.Session.ID != "abc12345" || got.Session.Turns != 3 || got.Session.TotalCost != 0.03 {
		t.Fatalf("unexpected session payload: %+v", got.Session)
	}
	if got.Message != "Agent finished." || got.DumpCommand != "agentctl dump abc12345" {
		t.Fatalf("unexpected message fields: %+v", got)
	}
}

func TestSendCompletionCommandOmitsEmptyName(t *testing.T) {
	data, err := json.Marshal(CompletionCommandPayload{
		SchemaVersion: 1,
		Event:         "session.completed",
		Session: CompletionCommandSession{
			ID:        "abc12345",
			Model:     "model",
			StartedAt: time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(data), `"name"`) {
		t.Fatalf("expected empty session name to be omitted, got %s", data)
	}
}

func TestSendCompletionCommandReportsNonZeroOutput(t *testing.T) {
	dir := t.TempDir()
	command := writeNotifierScript(t, dir, "fail.sh", `#!/bin/sh
echo "useful stdout"
echo "useful stderr" >&2
exit 7
`)

	err := SendCompletionCommand(command, CompletionCommandPayload{SchemaVersion: 1, Event: "session.completed"})
	if err == nil {
		t.Fatal("expected non-zero notifier to return error")
	}
	text := err.Error()
	if !strings.Contains(text, "exit status 7") || !strings.Contains(text, "useful stdout") || !strings.Contains(text, "useful stderr") {
		t.Fatalf("expected exit status and captured output in error, got %v", err)
	}
}

func TestSendCompletionCommandTimesOut(t *testing.T) {
	dir := t.TempDir()
	command := writeNotifierScript(t, dir, "sleep.sh", `#!/bin/sh
sleep 2
`)

	err := SendCompletionCommandWithTimeout(command, CompletionCommandPayload{SchemaVersion: 1, Event: "session.completed"}, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestSendCompletionCommandsAttemptsAllCommands(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.txt")
	secondPath := filepath.Join(dir, "second.txt")
	failCommand := writeNotifierScript(t, dir, "fail.sh", `#!/bin/sh
echo first > "$AGENTCTL_FIRST_OUT"
exit 3
`)
	okCommand := writeNotifierScript(t, dir, "ok.sh", `#!/bin/sh
echo second > "$AGENTCTL_SECOND_OUT"
`)
	t.Setenv("AGENTCTL_FIRST_OUT", firstPath)
	t.Setenv("AGENTCTL_SECOND_OUT", secondPath)

	err := SendCompletionCommands([]string{failCommand, okCommand}, CompletionCommandPayload{SchemaVersion: 1, Event: "session.completed"})
	if err == nil {
		t.Fatal("expected joined error from failing command")
	}
	if _, readErr := os.ReadFile(firstPath); readErr != nil {
		t.Fatalf("expected first notifier to run: %v", readErr)
	}
	if _, readErr := os.ReadFile(secondPath); readErr != nil {
		t.Fatalf("expected second notifier to run despite first failure: %v", readErr)
	}
}

func TestValidateCompletionCommandRequiresExplicitPath(t *testing.T) {
	if err := ValidateCompletionCommand("notify-test"); err == nil {
		t.Fatal("expected bare command name to be rejected")
	}
	if err := ValidateCompletionCommand("./notify-test --arg"); err == nil {
		t.Fatal("expected command string with arguments to be rejected")
	}
	if err := ValidateCompletionCommand("./notify-test"); err != nil {
		t.Fatalf("expected relative path to be accepted: %v", err)
	}
	if err := ValidateCompletionCommand("/tmp/notify-test"); err != nil {
		t.Fatalf("expected absolute path to be accepted: %v", err)
	}
}

func writeNotifierScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) returned error: %v", name, err)
	}
	return path
}
