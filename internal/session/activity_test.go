package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatEventStatusThinkingStart(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "Thinking..." {
		t.Fatalf("expected Thinking... status, got %q", status)
	}
}

func TestFormatEventStatusTurnEndIsSilent(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"turn_end","message":{"usage":{"totalTokens":12400,"cost":{"total":0.0312}}}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	turnCount := 1
	status := FormatEventStatus(event, &turnCount)
	if status != "" {
		t.Fatalf("expected empty status for turn_end, got %q", status)
	}
}

func TestFormatEventStatusToolError(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"tool_execution_end","isError":true}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "tool error" {
		t.Fatalf("expected tool error status, got %q", status)
	}
}

func TestFormatEventStatusToolRead(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"cmd/watch.go"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "→ Read cmd/watch.go" {
		t.Fatalf("expected read status, got %q", status)
	}
}

func TestParseActivityEventTextEndUsesFinalAssistantTextAndReplace(t *testing.T) {
	var event map[string]interface{}
	content := "Sure! Let me write the script and run it in one go."
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"`+content+`"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	activity := ParseActivityEvent(event, new(int))
	if activity.Status != content {
		t.Fatalf("expected final assistant text status, got %q", activity.Status)
	}
	if !activity.Replace {
		t.Fatal("expected text_end to set Replace")
	}
}

func TestFormatEventStatusTextEndPreservesFormattingAndTruncatesForSlack(t *testing.T) {
	var event map[string]interface{}
	longText := strings.Repeat("a", 3100)
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"`+longText+`"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status == "" {
		t.Fatal("expected non-empty status")
	}
	if len([]rune(status)) != 3000 {
		t.Fatalf("expected status truncated to 3000 runes, got %d", len([]rune(status)))
	}
	if !strings.HasSuffix(status, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", status[len(status)-5:])
	}
}

func TestParseActivityEventUnknownType(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"some_future_event","data":"whatever"}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	activity := ParseActivityEvent(event, new(int))
	if activity.State != "" || activity.Status != "" {
		t.Fatalf("expected empty activity for unknown event type, got state=%q status=%q", activity.State, activity.Status)
	}
}

func TestParseActivityEventAgentEnd(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"agent_end","messages":[]}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	turnCount := 3
	activity := ParseActivityEvent(event, &turnCount)
	if activity.State != "finished (3 turns)" {
		t.Fatalf("expected finished state, got %q", activity.State)
	}
}

func TestParseActivityEventAgentEndZeroTurns(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"agent_end","messages":[]}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	turnCount := 0
	activity := ParseActivityEvent(event, &turnCount)
	if activity.State != "finished (0 turns)" {
		t.Fatalf("expected finished 0 turns, got %q", activity.State)
	}
}

func TestParseActivityEventNilTurnCount(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"turn_end"}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	// nil turnCount should not panic
	activity := ParseActivityEvent(event, nil)
	if activity.State != "completed turn 1" {
		t.Fatalf("expected completed turn 1 with nil counter, got %q", activity.State)
	}
}

func TestParseActivityEventToolExecutionEndSuccess(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"tool_execution_end","isError":false}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	activity := ParseActivityEvent(event, new(int))
	if activity.State != "writing" {
		t.Fatalf("expected writing state after tool success, got %q", activity.State)
	}
}

func TestParseActivityLineInvalidJSON(t *testing.T) {
	activity := ParseActivityLine("not json", new(int))
	if activity.State != "" {
		t.Fatalf("expected empty activity for invalid JSON, got %q", activity.State)
	}
}

func TestParseActivityLineEmptyLine(t *testing.T) {
	activity := ParseActivityLine("", new(int))
	if activity.State != "" {
		t.Fatalf("expected empty activity for empty line, got %q", activity.State)
	}
}

func TestTruncateActivityText(t *testing.T) {
	// Short text unchanged
	if got := truncateActivityText("hello", 60); got != "hello" {
		t.Fatalf("expected unchanged, got %q", got)
	}
	// Newlines collapsed
	if got := truncateActivityText("line1\nline2", 60); got != "line1 line2" {
		t.Fatalf("expected newlines replaced, got %q", got)
	}
	// Whitespace trimmed
	if got := truncateActivityText("  hello  ", 60); got != "hello" {
		t.Fatalf("expected trimmed, got %q", got)
	}
	// Long text truncated
	long := strings.Repeat("x", 100)
	got := truncateActivityText(long, 60)
	if len(got) > 60 {
		t.Fatalf("expected truncated to 60, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ... suffix, got %q", got)
	}
}

func TestParseActivityEventThinkingEndIsSilent(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","content":"summary"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	activity := ParseActivityEvent(event, new(int))
	if activity.Status != "" {
		t.Fatalf("expected empty status for thinking_end, got %q", activity.Status)
	}
}
