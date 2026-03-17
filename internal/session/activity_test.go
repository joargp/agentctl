package session

import (
	"encoding/json"
	"testing"
)

func TestFormatEventStatusThinkingStart(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "thinking" {
		t.Fatalf("expected thinking status, got %q", status)
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

func TestFormatEventStatusTextEndUsesVisibleAssistantText(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"Sure! Let me write the script and run it in one go."}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "Let me write the script and run it in one go." {
		t.Fatalf("expected visible assistant text status, got %q", status)
	}
}

func TestFormatEventStatusTextEndIgnoresLongAssistantText(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"Everything worked perfectly. Here's a summary of what was done: first I wrote the file, then I ran the command, then I summarized the results in detail."}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status != "" {
		t.Fatalf("expected long assistant text to be ignored, got %q", status)
	}
}
