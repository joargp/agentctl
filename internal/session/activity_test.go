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
	if status != "💭 Thinking..." {
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
	if status != "❌ Tool error" {
		t.Fatalf("expected tool error status, got %q", status)
	}
}
