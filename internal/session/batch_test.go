package session

import (
	"encoding/json"
	"testing"
)

func TestParseBatchableDeltaToolcallDelta(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"pa"}}`)
	key, delta, evt, ok := ParseBatchableDelta(input)
	if !ok {
		t.Fatal("expected toolcall_delta to be batchable")
	}
	if key.AeType != "toolcall_delta" {
		t.Fatalf("expected AeType=toolcall_delta, got %q", key.AeType)
	}
	if key.ContentIndex != 1 {
		t.Fatalf("expected ContentIndex=1, got %v", key.ContentIndex)
	}
	if delta != `{"pa` {
		t.Fatalf("expected delta, got %q", delta)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
}

func TestParseBatchableDeltaTextDeltaNotBatchable(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hi"}}`)
	_, _, _, ok := ParseBatchableDelta(input)
	if ok {
		t.Fatal("text_delta should NOT be batchable")
	}
}

func TestParseBatchableDeltaThinkingDeltaNotBatchable(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"hmm"}}`)
	_, _, _, ok := ParseBatchableDelta(input)
	if ok {
		t.Fatal("thinking_delta should NOT be batchable")
	}
}

func TestParseBatchableDeltaNonDeltaEvent(t *testing.T) {
	input := []byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"ls"}}`)
	_, _, _, ok := ParseBatchableDelta(input)
	if ok {
		t.Fatal("tool_execution_start should not be batchable")
	}
}

func TestParseBatchableDeltaInvalidJSON(t *testing.T) {
	_, _, _, ok := ParseBatchableDelta([]byte("not json"))
	if ok {
		t.Fatal("invalid JSON should not be batchable")
	}
}

func TestParseBatchableDeltaNoAssistantMessageEvent(t *testing.T) {
	input := []byte(`{"type":"message_update"}`)
	_, _, _, ok := ParseBatchableDelta(input)
	if ok {
		t.Fatal("message_update without assistantMessageEvent should not be batchable")
	}
}

func TestMarshalBatchedDeltaNested(t *testing.T) {
	event := map[string]interface{}{
		"type": "message_update",
		"assistantMessageEvent": map[string]interface{}{
			"type":         "toolcall_delta",
			"contentIndex": float64(1),
			"delta":        "original",
		},
	}
	result := MarshalBatchedDelta(event, `{"command":"ls -la"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Verify the delta was replaced
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ae, _ := parsed["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatal("expected assistantMessageEvent")
	}
	if ae["delta"] != `{"command":"ls -la"}` {
		t.Fatalf("expected merged delta, got %v", ae["delta"])
	}
	// Must end with newline
	if result[len(result)-1] != '\n' {
		t.Fatal("expected trailing newline")
	}
}

func TestMarshalBatchedDeltaTopLevel(t *testing.T) {
	event := map[string]interface{}{
		"type":  "toolcall_delta",
		"delta": "old",
	}
	result := MarshalBatchedDelta(event, "new_value")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["delta"] != "new_value" {
		t.Fatalf("expected top-level delta replaced, got %v", parsed["delta"])
	}
}

func TestDeltaKeyEquality(t *testing.T) {
	k1 := DeltaKey{AeType: "toolcall_delta", ContentIndex: 1}
	k2 := DeltaKey{AeType: "toolcall_delta", ContentIndex: 1}
	k3 := DeltaKey{AeType: "toolcall_delta", ContentIndex: 2}
	if k1 != k2 {
		t.Fatal("same keys should be equal")
	}
	if k1 == k3 {
		t.Fatal("different contentIndex keys should not be equal")
	}
}
