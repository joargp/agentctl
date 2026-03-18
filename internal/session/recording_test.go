package session

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSanitizeRecordingLineRemovesHeavyFieldsFromDeltaEvents(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"plan","partial":"plan so far","message":{"role":"assistant"}}}` + "\n")

	got := SanitizeRecordingLine(input)
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatalf("expected trailing newline to be preserved, got %q", string(got))
	}

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal sanitized line: %v", err)
	}

	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatalf("expected assistantMessageEvent, got %v", event)
	}
	if _, ok := ae["partial"]; ok {
		t.Fatalf("expected partial field to be removed, got %v", ae)
	}
	if _, ok := ae["message"]; ok {
		t.Fatalf("expected message field to be removed, got %v", ae)
	}
	if ae["type"] != "thinking_delta" || ae["delta"] != "plan" {
		t.Fatalf("expected delta event fields to remain, got %v", ae)
	}
}

func TestSanitizeRecordingLineLeavesOtherEventsUnchanged(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"done"}}` + "\n")
	got := SanitizeRecordingLine(input)
	if string(got) != string(input) {
		t.Fatalf("expected non-delta event to be unchanged, got %q", string(got))
	}
}
