package cmd

import (
	"strings"
	"testing"
)

func TestRenderJSONLineToolExecution(t *testing.T) {
	input := `{"type":"tool_execution_start","toolName":"bash","args":{"command":"ls -la"}}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "🔧 bash") {
		t.Fatalf("expected bash tool call, got %q", result)
	}
	if !strings.Contains(result, "ls -la") {
		t.Fatalf("expected command in output, got %q", result)
	}
}

func TestRenderJSONLineToolError(t *testing.T) {
	input := `{"type":"tool_execution_end","isError":true}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "❌") {
		t.Fatalf("expected error indicator, got %q", result)
	}
}

func TestRenderJSONLineToolSuccess(t *testing.T) {
	input := `{"type":"tool_execution_end","isError":false}`
	result := renderJSONLine(input)
	if result != "" {
		t.Fatalf("expected empty for tool success, got %q", result)
	}
}

func TestRenderJSONLineTurnEnd(t *testing.T) {
	input := `{"type":"turn_end","message":{"usage":{"totalTokens":5000,"cost":{"total":0.015}}}}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "5000 tokens") {
		t.Fatalf("expected token count, got %q", result)
	}
	if !strings.Contains(result, "$0.0150") {
		t.Fatalf("expected cost, got %q", result)
	}
	if !strings.Contains(result, "---") {
		t.Fatalf("expected separator, got %q", result)
	}
}

func TestRenderJSONLineTurnEndNoUsage(t *testing.T) {
	input := `{"type":"turn_end"}`
	result := renderJSONLine(input)
	if result != "---" {
		t.Fatalf("expected plain separator, got %q", result)
	}
}

func TestRenderJSONLineThinkingStart(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}`
	result := renderJSONLine(input)
	if result != "💭 thinking..." {
		t.Fatalf("expected thinking indicator, got %q", result)
	}
}

func TestRenderJSONLineTopLevelThinkingStart(t *testing.T) {
	input := `{"type":"thinking_start"}`
	result := renderJSONLine(input)
	if result != "💭 thinking..." {
		t.Fatalf("expected thinking indicator for top-level event, got %q", result)
	}
}

func TestRenderJSONLineToolUpdate(t *testing.T) {
	input := `{"type":"tool_execution_update","partialResult":{"content":[{"type":"text","text":"partial output here"}]}}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "partial output here") {
		t.Fatalf("expected partial result text, got %q", result)
	}
	// Should not have the "→ " prefix (replaced with "  ")
	if strings.HasPrefix(result, "→") {
		t.Fatalf("expected '  ' prefix not '→', got %q", result)
	}
	// Should not have trailing newline (monitor adds its own formatting)
	if strings.HasSuffix(result, "\n") {
		t.Fatalf("expected no trailing newline, got %q", result)
	}
	if result != "  partial output here" {
		t.Fatalf("expected exact formatted output, got %q", result)
	}
}

func TestRenderJSONLineInvalidJSON(t *testing.T) {
	result := renderJSONLine("not json")
	if result != "" {
		t.Fatalf("expected empty for invalid JSON, got %q", result)
	}
}

func TestRenderJSONLineUnknownEvent(t *testing.T) {
	result := renderJSONLine(`{"type":"session","version":3}`)
	if result != "" {
		t.Fatalf("expected empty for unknown event, got %q", result)
	}
}

func TestClassifyEventTextDelta(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`
	delta, kind, other := classifyEvent(input)
	if delta != "hello" {
		t.Fatalf("expected delta 'hello', got %q", delta)
	}
	if kind != "text" {
		t.Fatalf("expected kind 'text', got %q", kind)
	}
	if other != "" {
		t.Fatalf("expected empty other, got %q", other)
	}
}

func TestClassifyEventThinkingDelta(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"let me think"}}`
	delta, kind, other := classifyEvent(input)
	if delta != "let me think" {
		t.Fatalf("expected delta, got %q", delta)
	}
	if kind != "thinking" {
		t.Fatalf("expected kind 'thinking', got %q", kind)
	}
	if other != "" {
		t.Fatalf("expected empty other, got %q", other)
	}
}

func TestClassifyEventTopLevelTextDelta(t *testing.T) {
	input := `{"type":"text_delta","delta":"hi there"}`
	delta, kind, _ := classifyEvent(input)
	if delta != "hi there" || kind != "text" {
		t.Fatalf("expected text delta, got delta=%q kind=%q", delta, kind)
	}
}

func TestClassifyEventTopLevelThinkingDelta(t *testing.T) {
	input := `{"type":"thinking_delta","delta":"hmm"}`
	delta, kind, _ := classifyEvent(input)
	if delta != "hmm" || kind != "thinking" {
		t.Fatalf("expected thinking delta, got delta=%q kind=%q", delta, kind)
	}
}

func TestClassifyEventNonDelta(t *testing.T) {
	input := `{"type":"tool_execution_start","toolName":"read","args":{"path":"foo.go"}}`
	delta, kind, other := classifyEvent(input)
	if delta != "" || kind != "" {
		t.Fatalf("expected no delta for tool start, got delta=%q kind=%q", delta, kind)
	}
	if !strings.Contains(other, "🔧 read") {
		t.Fatalf("expected tool call in other, got %q", other)
	}
}

func TestClassifyEventInvalidJSON(t *testing.T) {
	delta, kind, other := classifyEvent("not json at all")
	if delta != "" || kind != "" || other != "" {
		t.Fatalf("expected all empty for invalid JSON, got delta=%q kind=%q other=%q", delta, kind, other)
	}
}

func TestRenderJSONLineReadTool(t *testing.T) {
	input := `{"type":"tool_execution_start","toolName":"read","args":{"path":"/src/main.go"}}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "🔧 read: /src/main.go") {
		t.Fatalf("expected read tool call with path, got %q", result)
	}
}

func TestRenderJSONLineTodoTool(t *testing.T) {
	input := `{"type":"tool_execution_start","toolName":"todo","args":{"action":"create","title":"Fix bug"}}`
	result := renderJSONLine(input)
	if !strings.Contains(result, "todo create") {
		t.Fatalf("expected todo tool, got %q", result)
	}
}
