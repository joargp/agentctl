package session

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSanitizeRecordingLineRemovesHeavyFieldsFromDeltaEvents(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"plan","partial":"plan so far","message":{"role":"assistant"}},"message":{"role":"assistant","content":[{"type":"thinking","thinking":"plan so far"}]}}` + "\n")

	got := SanitizeRecordingLine(input)
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatalf("expected trailing newline to be preserved, got %q", string(got))
	}

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal sanitized line: %v", err)
	}

	// Top-level "message" must be stripped.
	if _, ok := event["message"]; ok {
		t.Fatalf("expected top-level message field to be removed, got %v", event)
	}

	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatalf("expected assistantMessageEvent, got %v", event)
	}
	if _, ok := ae["partial"]; ok {
		t.Fatalf("expected partial field to be removed, got %v", ae)
	}
	if _, ok := ae["message"]; ok {
		t.Fatalf("expected ae.message field to be removed, got %v", ae)
	}
	if ae["type"] != "thinking_delta" || ae["delta"] != "plan" {
		t.Fatalf("expected delta event fields to remain, got %v", ae)
	}
}

func TestSanitizeRecordingLineStripsToolcallDelta(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"co","partial":"{\"command\":\"ls"},"message":{"role":"assistant","content":[{"type":"thinking","thinking":"let me check"}]}}` + "\n")

	got := SanitizeRecordingLine(input)

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal sanitized line: %v", err)
	}

	if _, ok := event["message"]; ok {
		t.Fatalf("expected top-level message to be removed")
	}

	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatal("expected assistantMessageEvent")
	}
	if _, ok := ae["partial"]; ok {
		t.Fatalf("expected partial to be removed")
	}
	// delta must be preserved
	if ae["delta"] != `{"co` {
		t.Fatalf("expected delta to be preserved, got %v", ae["delta"])
	}
	if ae["type"] != "toolcall_delta" {
		t.Fatalf("expected type to be preserved, got %v", ae["type"])
	}
}

func TestSanitizeRecordingLineStripsNonUserMessageStartContent(t *testing.T) {
	// Non-user message_start content (toolResult, assistant, custom, system) is never
	// displayed by dump — only role=="user" is rendered. Strip to avoid recording
	// tool results and context documents verbatim.
	roles := []string{"assistant", "toolResult", "custom", "system"}
	for _, role := range roles {
		input := []byte(`{"type":"message_start","message":{"role":"` + role + `","content":[{"type":"text","text":"large content here"}],"timestamp":1234}}` + "\n")
		got := SanitizeRecordingLine(input)

		var event map[string]interface{}
		if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
			t.Fatalf("role %s: unmarshal: %v", role, err)
		}
		msg, _ := event["message"].(map[string]interface{})
		if msg == nil {
			t.Fatalf("role %s: expected message to remain", role)
		}
		if _, ok := msg["content"]; ok {
			t.Fatalf("role %s: expected content to be stripped from non-user message_start", role)
		}
		if msg["role"] != role {
			t.Fatalf("role %s: expected role to remain, got %v", role, msg["role"])
		}
	}
}

func TestSanitizeRecordingLinePreservesUserMessageStartContent(t *testing.T) {
	// User message content must be preserved — dump displays it as the task context.
	input := []byte(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"run the tests and fix any failures"}],"timestamp":1234}}` + "\n")
	got := SanitizeRecordingLine(input)
	if string(got) != string(input) {
		t.Fatalf("expected user message_start to be unchanged, got %q", string(got))
	}
}

func TestSanitizeRecordingLineStripsThinkingEndContent(t *testing.T) {
	// thinking_end.ae.content is fully assembled thinking text — already in thinking_delta events.
	// No consumer reads it.
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"Let me think about this carefully..."}}` + "\n")

	got := SanitizeRecordingLine(input)

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatal("expected assistantMessageEvent to remain")
	}
	if _, ok := ae["content"]; ok {
		t.Fatalf("expected ae.content to be stripped from thinking_end, got %v", ae)
	}
	if ae["type"] != "thinking_end" || ae["contentIndex"] != float64(0) {
		t.Fatalf("expected type and contentIndex to remain, got %v", ae)
	}
}

func TestSanitizeRecordingLineStripsToolcallEndToolCall(t *testing.T) {
	// toolcall_end.ae.toolCall is the fully assembled tool input — identical to
	// tool_execution_start.args which follows. Strip it from recordings.
	toolCallJSON := `{"type":"toolCall","id":"call_abc","name":"bash","input":{"command":"ls -la /repos"}}`
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":0,"toolCall":` + toolCallJSON + `}}` + "\n")

	got := SanitizeRecordingLine(input)

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		t.Fatal("expected assistantMessageEvent to remain")
	}
	if _, ok := ae["toolCall"]; ok {
		t.Fatalf("expected ae.toolCall to be stripped from toolcall_end, got %v", ae)
	}
	if ae["type"] != "toolcall_end" {
		t.Fatalf("expected type=toolcall_end to remain, got %v", ae["type"])
	}
	if ae["contentIndex"] != float64(0) {
		t.Fatalf("expected contentIndex to remain, got %v", ae["contentIndex"])
	}
}

func TestSanitizeRecordingLineStripsTopLevelMessageFromAllSubTypes(t *testing.T) {
	subtypes := []string{
		"thinking_delta", "text_delta", "toolcall_delta",
		"toolcall_start", "toolcall_end",
		"thinking_start", "thinking_end",
		"text_start", "text_end",
	}
	for _, st := range subtypes {
		input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"` + st + `"},"message":{"role":"assistant","content":[]}}` + "\n")
		got := SanitizeRecordingLine(input)

		var event map[string]interface{}
		if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
			t.Fatalf("subtype %s: unmarshal: %v", st, err)
		}
		if _, ok := event["message"]; ok {
			t.Fatalf("subtype %s: expected top-level message to be removed", st)
		}
	}
}

func TestSanitizeRecordingLineStripsMessageEndMessage(t *testing.T) {
	// message_end carries the fully-assembled content — redundant with deltas and turn_end.
	input := []byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello world"}],"timestamp":1234567890}}` + "\n")

	got := SanitizeRecordingLine(input)
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", string(got))
	}

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := event["message"]; ok {
		t.Fatalf("expected message field to be removed from message_end, got %v", event)
	}
	if event["type"] != "message_end" {
		t.Fatalf("expected type to remain message_end, got %v", event["type"])
	}
}

func TestSanitizeRecordingLineStripsToolExecutionUpdateArgs(t *testing.T) {
	// tool_execution_update.args are identical to tool_execution_start.args — strip them.
	// partialResult must be PRESERVED — monitor reads it for live tool output streaming.
	input := []byte(`{"type":"tool_execution_update","toolCallId":"call_abc","toolName":"bash","args":{"command":"ls -la"},"partialResult":{"content":[{"type":"text","text":"file1\nfile2\n"}]}}` + "\n")

	got := SanitizeRecordingLine(input)
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", string(got))
	}

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// args should be stripped (dup of tool_execution_start.args)
	if _, ok := event["args"]; ok {
		t.Fatalf("expected args to be stripped from tool_execution_update")
	}
	// partialResult must remain for monitor live streaming
	if _, ok := event["partialResult"]; !ok {
		t.Fatalf("expected partialResult to be PRESERVED in tool_execution_update (monitor needs it)")
	}
	// Identity fields must remain
	if event["toolName"] != "bash" {
		t.Fatalf("expected toolName=bash, got %v", event["toolName"])
	}
	if event["toolCallId"] != "call_abc" {
		t.Fatalf("expected toolCallId to remain, got %v", event["toolCallId"])
	}
}

func TestSanitizeRecordingLinePreserves_TurnEndContent(t *testing.T) {
	// turn_end.message.content is now preserved — used by completion notifications
	// and as a fallback for summary rendering when text_delta events are missing.
	input := []byte(`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"totalTokens":5000,"cost":{"total":0.01}},"stopReason":"end_turn"}}` + "\n")

	got := SanitizeRecordingLine(input)
	// turn_end is no longer modified, so output should equal input.
	if string(got) != string(input) {
		t.Fatalf("expected turn_end to pass through unchanged, got %q", string(got))
	}

	var event map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(got), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	msg, _ := event["message"].(map[string]interface{})
	if msg == nil {
		t.Fatal("expected message to remain on turn_end")
	}
	// content must now be preserved
	if _, ok := msg["content"]; !ok {
		t.Fatal("expected message.content to be preserved on turn_end")
	}
	// usage must still be preserved for cost tracking
	usage, _ := msg["usage"].(map[string]interface{})
	if usage == nil {
		t.Fatal("expected message.usage to be preserved")
	}
	if usage["totalTokens"].(float64) != 5000 {
		t.Fatalf("expected totalTokens=5000, got %v", usage["totalTokens"])
	}
}

func TestSanitizeRecordingLineLeavesNonMessageUpdateEventsUnchanged(t *testing.T) {
	// turn_end without content (no content to strip) must be unchanged
	input := []byte(`{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":1000}}}` + "\n")
	got := SanitizeRecordingLine(input)
	if string(got) != string(input) {
		t.Fatalf("expected turn_end without content to be unchanged, got %q", string(got))
	}
}

func TestSanitizeRecordingLineLeavesToolExecutionEventsUnchanged(t *testing.T) {
	input := []byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"ls"}}` + "\n")
	got := SanitizeRecordingLine(input)
	if string(got) != string(input) {
		t.Fatalf("expected tool event to be unchanged, got %q", string(got))
	}
}

func TestSanitizeRecordingLinePreservesMessageUpdateWithoutHeavyFields(t *testing.T) {
	// A message_update that has no message/partial fields — should still be valid
	// but return unchanged (no fields to strip -> no re-marshal needed).
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"done"}}` + "\n")
	got := SanitizeRecordingLine(input)
	if string(got) != string(input) {
		t.Fatalf("expected event with no heavy fields to be unchanged, got %q", string(got))
	}
}
