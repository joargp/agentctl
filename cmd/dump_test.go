package cmd

import (
	"strings"
	"testing"
)

func TestRenderJSONLogTextDeltas(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Hello"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":" world"}}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "Hello world") {
		t.Fatalf("expected 'Hello world' in output, got %q", result)
	}
}

func TestRenderJSONLogToolExecution(t *testing.T) {
	input := `{"type":"tool_execution_start","toolCallId":"abc","toolName":"bash","args":{"command":"echo hello"}}
{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"hello\n"}]},"isError":false}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "🔧 bash: echo hello") {
		t.Fatalf("expected tool call in output, got %q", result)
	}
	if !strings.Contains(result, "→ hello") {
		t.Fatalf("expected tool result in output, got %q", result)
	}
}

func TestRenderJSONLogToolError(t *testing.T) {
	input := `{"type":"tool_execution_start","toolCallId":"abc","toolName":"bash","args":{"command":"false"}}
{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":""}]},"isError":true}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "❌ error") {
		t.Fatalf("expected error indicator in output, got %q", result)
	}
}

func TestRenderJSONLogUserMessage(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Say hello"}]}}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "> Say hello") {
		t.Fatalf("expected user message in output, got %q", result)
	}
}

func TestRenderJSONLogTurnEnd(t *testing.T) {
	input := `{"type":"turn_end"}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "---") {
		t.Fatalf("expected turn separator in output, got %q", result)
	}
}

func TestRenderJSONLogReadTool(t *testing.T) {
	input := `{"type":"tool_execution_start","toolCallId":"abc","toolName":"read","args":{"path":"/foo/bar.go"}}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "🔧 read: /foo/bar.go") {
		t.Fatalf("expected read tool with path in output, got %q", result)
	}
}

func TestRenderJSONLogLongToolResult(t *testing.T) {
	longText := strings.Repeat("x", 300)
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"` + longText + `"}]},"isError":false}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "...") {
		t.Fatalf("expected truncation of long result, got len=%d", len(result))
	}
	if len(result) > 250 {
		// Should be truncated to ~200 chars
	}
}

func TestRenderJSONLogThinkingIndicator(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}
{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"Let me think..."}}
{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"Let me think..."}}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "💭 thinking...") {
		t.Fatalf("expected thinking indicator in output, got %q", result)
	}
}

func TestRenderJSONLogTurnEndWithUsage(t *testing.T) {
	input := `{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":5000,"cost":{"total":0.015}}}}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "5000 tokens") {
		t.Fatalf("expected token count in output, got %q", result)
	}
	if !strings.Contains(result, "$0.0150") {
		t.Fatalf("expected cost in output, got %q", result)
	}
}

func TestRenderJSONLogPlainTextFallback(t *testing.T) {
	// Plain text (non-JSON) should produce empty rendered output,
	// triggering the fallback path in runDump.
	input := `Hello, this is plain text output from the old TUI mode.
It has multiple lines.
No JSON here.
`
	result := renderJSONLog([]byte(input))
	if strings.TrimSpace(result) != "" {
		t.Fatalf("expected empty rendered output for plain text, got %q", result)
	}
}

func TestSplitLines(t *testing.T) {
	lines := splitLines([]byte("foo\nbar\nbaz"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "foo" || lines[1] != "bar" || lines[2] != "baz" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}
