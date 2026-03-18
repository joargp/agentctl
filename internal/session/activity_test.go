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

func TestFormatEventStatusTextEndTruncatesLongAssistantText(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"Everything worked perfectly. Here's a summary of what was done: first I wrote the file, then I ran the command, then I summarized the results in detail."}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	// Text under 200 chars should now be included as-is
	expected := "Everything worked perfectly. Here's a summary of what was done: first I wrote the file, then I ran the command, then I summarized the results in detail."
	if status != expected {
		t.Fatalf("expected full text (under 200 chars), got %q", status)
	}
}

func TestFormatEventStatusTextEndTruncatesVeryLongAssistantText(t *testing.T) {
	// Text over 200 chars should be truncated at a sentence/word boundary
	longText := "This is a comprehensive analysis of the problem at hand. The root cause appears to be a race condition in the authentication middleware that occurs when multiple requests arrive simultaneously. I've identified three potential fixes that we should evaluate carefully before proceeding."
	var event map[string]interface{}
	payload := `{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"` + longText + `"}}`
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := FormatEventStatus(event, new(int))
	if status == "" {
		t.Fatal("expected non-empty truncated status for very long text")
	}
	if len(status) > 210 {
		t.Fatalf("expected truncated status under 210 chars, got %d chars: %q", len(status), status)
	}
	if status == longText {
		t.Fatal("expected text to be truncated, but got full text")
	}
}
