package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordStreamSanitizesLogButPreservesStdout(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":" world","partial":"Hello world","message":{"role":"assistant","content":[]}}}`,
		`plain stderr line`,
		"",
	}, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	if stdout.String() != input {
		t.Fatalf("expected stdout passthrough, got %q", stdout.String())
	}

	logged := log.String()
	if strings.Contains(logged, `"partial"`) {
		t.Fatalf("expected sanitized log to drop partial field, got %q", logged)
	}
	// "message" as a key should be stripped from message_update events.
	// The logged event will have been batched (single event flushed at EOF).
	if !strings.Contains(logged, `"delta"`) {
		t.Fatalf("expected sanitized log to keep delta, got %q", logged)
	}
	if strings.Contains(logged, "plain stderr line") {
		t.Fatalf("expected non-JSON lines to be filtered from log, got %q", logged)
	}

	// Verify the delta content is present.
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(logged)), &event); err != nil {
		t.Fatalf("unmarshal logged event: %v", err)
	}
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatalf("expected assistantMessageEvent in logged output")
	}
	if ae["delta"] != " world" {
		t.Fatalf("expected delta=' world', got %v", ae["delta"])
	}
}

func TestRecordStreamBatchesConsecutiveDeltas(t *testing.T) {
	// Three consecutive toolcall_delta events with the same contentIndex should
	// be merged into a single event with concatenated delta strings.
	lines := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"pa"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"th\":\""}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"a.txt\""}}`,
		"",
	}
	input := strings.Join(lines, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	// stdout must have all original events.
	if stdout.String() != input {
		t.Fatalf("expected stdout passthrough, got %q", stdout.String())
	}

	// Log should have exactly 1 batched event.
	logged := strings.TrimSpace(log.String())
	logLines := strings.Split(logged, "\n")
	if len(logLines) != 1 {
		t.Fatalf("expected 1 batched log line, got %d: %q", len(logLines), logged)
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(logLines[0]), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatalf("expected assistantMessageEvent")
	}
	if ae["delta"] != `{"path":"a.txt"` {
		t.Fatalf("expected concatenated delta, got %v", ae["delta"])
	}
}

func TestRecordStreamFlushesOnTypeChange(t *testing.T) {
	// Batched toolcall_delta events are flushed when a non-delta event arrives.
	lines := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"aa"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"bb"}}`,
		`{"type":"tool_execution_start","toolName":"bash","args":{"command":"ls"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"cc"}}`,
		"",
	}
	input := strings.Join(lines, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	logged := strings.TrimSpace(log.String())
	logLines := strings.Split(logged, "\n")
	// Expect 3 events: batched toolcall_delta, tool_execution_start, second toolcall_delta
	if len(logLines) != 3 {
		t.Fatalf("expected 3 log lines, got %d: %q", len(logLines), logged)
	}

	// First: batched "aabb"
	var e1 map[string]interface{}
	json.Unmarshal([]byte(logLines[0]), &e1)
	ae1, _ := e1["assistantMessageEvent"].(map[string]interface{})
	if ae1["delta"] != "aabb" {
		t.Fatalf("expected first batch 'aabb', got %v", ae1["delta"])
	}

	// Second: tool_execution_start
	var e2 map[string]interface{}
	json.Unmarshal([]byte(logLines[1]), &e2)
	if e2["type"] != "tool_execution_start" {
		t.Fatalf("expected tool_execution_start, got %v", e2["type"])
	}

	// Third: "cc" (single event flushed at EOF)
	var e3 map[string]interface{}
	json.Unmarshal([]byte(logLines[2]), &e3)
	ae3, _ := e3["assistantMessageEvent"].(map[string]interface{})
	if ae3["delta"] != "cc" {
		t.Fatalf("expected 'cc', got %v", ae3["delta"])
	}
}

func TestRecordStreamDoesNotBatchTextDelta(t *testing.T) {
	// text_delta must NOT be batched — dump --follow and monitor need them
	// to appear incrementally for live output.
	lines := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":" world"}}`,
		"",
	}
	input := strings.Join(lines, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	logged := strings.TrimSpace(log.String())
	logLines := strings.Split(logged, "\n")
	// Each text_delta should be written individually (not batched)
	if len(logLines) != 2 {
		t.Fatalf("expected 2 log lines (unbatched text_delta), got %d: %q", len(logLines), logged)
	}
}

func TestRecordStreamFlushesOnContentIndexChange(t *testing.T) {
	// Different contentIndex values should not be batched together.
	lines := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"aa"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"bb"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":3,"delta":"cc"}}`,
		"",
	}
	input := strings.Join(lines, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	logged := strings.TrimSpace(log.String())
	logLines := strings.Split(logged, "\n")
	if len(logLines) != 2 {
		t.Fatalf("expected 2 log lines (different contentIndex), got %d: %q", len(logLines), logged)
	}

	var e1 map[string]interface{}
	json.Unmarshal([]byte(logLines[0]), &e1)
	ae1, _ := e1["assistantMessageEvent"].(map[string]interface{})
	if ae1["delta"] != "aabb" {
		t.Fatalf("expected 'aabb', got %v", ae1["delta"])
	}

	var e2 map[string]interface{}
	json.Unmarshal([]byte(logLines[1]), &e2)
	ae2, _ := e2["assistantMessageEvent"].(map[string]interface{})
	if ae2["delta"] != "cc" {
		t.Fatalf("expected 'cc', got %v", ae2["delta"])
	}
}

func TestRecordStreamFiltersTerminalEscapeSequences(t *testing.T) {
	// Simulate the OSC notification that pi emits on stderr.
	oscLine := "\x1b]777;notify;\xcf\x80;Agent finished with a very long summary...\n"
	jsonLine := `{"type":"turn_end","message":{"role":"assistant"}}` + "\n"

	input := jsonLine + oscLine

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	if stdout.String() != input {
		t.Fatalf("expected stdout passthrough, got %q", stdout.String())
	}

	logged := log.String()
	if !strings.Contains(logged, `"turn_end"`) {
		t.Fatalf("expected JSON line in log, got %q", logged)
	}
	if strings.Contains(logged, "notify") {
		t.Fatalf("expected OSC escape sequence to be filtered from log, got %q", logged)
	}
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`{"type":"test"}`, true},
		{`  {"type":"test"}`, true},
		{"\t{}\n", true},
		{`plain text`, false},
		{"\x1b]777;notify;test\n", false},
		{"", false},
		{"  \n", false},
	}
	for _, tt := range tests {
		got := looksLikeJSON([]byte(tt.input))
		if got != tt.expected {
			t.Errorf("looksLikeJSON(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
