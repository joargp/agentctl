package cmd

import (
	"testing"
)

func TestParseLastActivityThinking(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}
{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"Let me think"}}
`)
	state, detail := parseLastActivity(input)
	if state != "thinking" {
		t.Fatalf("expected 'thinking', got %q", state)
	}
	if detail != "" {
		t.Fatalf("expected empty detail, got %q", detail)
	}
}

func TestParseLastActivityRunningBash(t *testing.T) {
	input := []byte(`{"type":"tool_execution_start","toolCallId":"abc","toolName":"bash","args":{"command":"echo hello"}}
`)
	state, detail := parseLastActivity(input)
	if state != "running bash" {
		t.Fatalf("expected 'running bash', got %q", state)
	}
	if detail != "echo hello" {
		t.Fatalf("expected 'echo hello', got %q", detail)
	}
}

func TestParseLastActivityWriting(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Hello world"}}
`)
	state, detail := parseLastActivity(input)
	if state != "writing" {
		t.Fatalf("expected 'writing', got %q", state)
	}
	if detail != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", detail)
	}
}

func TestParseLastActivityTurnEnd(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"turn_end"}
`)
	state, _ := parseLastActivity(input)
	if state != "completed turn 1" {
		t.Fatalf("expected 'completed turn 1', got %q", state)
	}
}

func TestParseLastActivityAgentEnd(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"turn_end"}
{"type":"agent_end","messages":[]}
`)
	state, _ := parseLastActivity(input)
	if state != "finished (2 turns)" {
		t.Fatalf("expected 'finished (2 turns)', got %q", state)
	}
}

func TestParseLastActivityEmpty(t *testing.T) {
	state, _ := parseLastActivity([]byte(""))
	if state != "starting" {
		t.Fatalf("expected 'starting', got %q", state)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 20) != "short" {
		t.Fatal("short string should not be truncated")
	}
	result := truncate("this is a very long string that should be truncated", 20)
	if len(result) > 20 {
		t.Fatalf("expected max 20 chars, got %d", len(result))
	}
	if result[len(result)-3:] != "..." {
		t.Fatal("expected truncation suffix ...")
	}
}
