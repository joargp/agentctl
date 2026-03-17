package cmd

import (
	"encoding/json"
	"testing"

	sessionpkg "github.com/joargp/agentctl/internal/session"
)

func TestParseLastActivityThinking(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}
{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"Let me think"}}
`)
	state, detail := sessionpkg.ParseLastActivity(input)
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
	state, detail := sessionpkg.ParseLastActivity(input)
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
	state, detail := sessionpkg.ParseLastActivity(input)
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
	state, _ := sessionpkg.ParseLastActivity(input)
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
	state, _ := sessionpkg.ParseLastActivity(input)
	if state != "finished (2 turns)" {
		t.Fatalf("expected 'finished (2 turns)', got %q", state)
	}
}

func TestParseLastActivityEmpty(t *testing.T) {
	state, _ := sessionpkg.ParseLastActivity([]byte(""))
	if state != "starting" {
		t.Fatalf("expected 'starting', got %q", state)
	}
}

func TestFormatEventStatus(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"cmd/watch.go"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := sessionpkg.FormatEventStatus(event, new(int))
	if status != "reading `cmd/watch.go`" {
		t.Fatalf("expected progress status for read, got %q", status)
	}
}

func TestFormatEventStatusIgnoresTextDelta(t *testing.T) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Hello"}}`), &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	status := sessionpkg.FormatEventStatus(event, new(int))
	if status != "" {
		t.Fatalf("expected empty status for text_delta, got %q", status)
	}
}
