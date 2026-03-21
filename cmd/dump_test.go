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

func TestRenderJSONLogSummaryFallsBackToTopLevelTextEndContent(t *testing.T) {
	long := strings.Repeat("A", 200)
	input := `{"type":"text_end","content":"` + long + `"}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, long) {
		t.Fatalf("expected text_end content in summary output, got %q", result)
	}
}

func TestRenderJSONLogSummaryFallsBackToNestedTextEndContent(t *testing.T) {
	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_end","content":"Final answer"}}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "Final answer") {
		t.Fatalf("expected nested text_end content in summary output, got %q", result)
	}
}

func TestRenderJSONLogSummaryShowsUserMessage(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Fix the bug"}]}}
{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Done"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Done"}}
{"type":"turn_end"}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "> Fix the bug") {
		t.Fatalf("expected user message in summary, got %q", result)
	}
	if !strings.Contains(result, "Done") {
		t.Fatalf("expected assistant text in summary, got %q", result)
	}
}

func TestRenderJSONLogSummaryTruncatesLongUserMessage(t *testing.T) {
	longMsg := strings.Repeat("x", 300)
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"` + longMsg + `"}]}}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, ">") {
		t.Fatalf("expected user message prefix in summary, got %q", result)
	}
	if !strings.Contains(result, "...") {
		t.Fatalf("expected truncation of long user message, got %q", result)
	}
}

func TestRenderJSONLogSummaryShowsTurnCostInfo(t *testing.T) {
	input := `{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":5000,"cost":{"total":0.015}}}}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "turn 1") {
		t.Fatalf("expected turn number in summary, got %q", result)
	}
	if !strings.Contains(result, "5000 tokens") {
		t.Fatalf("expected token count in summary, got %q", result)
	}
	if !strings.Contains(result, "$0.0150") {
		t.Fatalf("expected cost in summary, got %q", result)
	}
}

func TestRenderJSONLogSummaryToolResultTruncationAt500(t *testing.T) {
	longText := strings.Repeat("y", 600)
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"` + longText + `"}]},"isError":false}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "...") {
		t.Fatalf("expected truncation of long tool result in summary")
	}
	// Should be truncated at 500 chars, not 200
	if len(result) < 400 {
		t.Fatalf("expected longer tool result in summary (500 char limit), got len=%d", len(result))
	}
}

func TestRenderJSONLineForDumpHandlesTopLevelTextDelta(t *testing.T) {
	input := `{"type":"text_delta","contentIndex":0,"delta":"Hello world"}`
	result := renderJSONLineForDump(input)
	if result != "Hello world" {
		t.Fatalf("expected top-level text delta to render, got %q", result)
	}
}

func TestRenderJSONLineForDumpHandlesTopLevelThinkingStart(t *testing.T) {
	input := `{"type":"thinking_start","contentIndex":0}`
	result := renderJSONLineForDump(input)
	if result != "💭 thinking...\n" {
		t.Fatalf("expected top-level thinking_start to render, got %q", result)
	}
}

func TestRenderJSONLogToolErrorShowsMessage(t *testing.T) {
	input := `{"type":"tool_execution_start","toolCallId":"abc","toolName":"bash","args":{"command":"cat /missing"}}
{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"cat: /missing: No such file"}]},"isError":true}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "❌") {
		t.Fatalf("expected error indicator, got %q", result)
	}
	if !strings.Contains(result, "No such file") {
		t.Fatalf("expected error message in output, got %q", result)
	}
}

func TestRenderJSONLogSummaryToolErrorShowsMessage(t *testing.T) {
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"permission denied"}]},"isError":true}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "❌") {
		t.Fatalf("expected error indicator, got %q", result)
	}
	if !strings.Contains(result, "permission denied") {
		t.Fatalf("expected error message in summary, got %q", result)
	}
}

func TestFilterLastNTurns(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"turn1"}}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"turn2"}}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"turn3"}}
{"type":"turn_end"}
`)
	result := filterLastNTurns(input, 1)
	rendered := renderJSONLog(result)
	if strings.Contains(rendered, "turn1") || strings.Contains(rendered, "turn2") {
		t.Fatalf("expected only last turn, got %q", rendered)
	}
	if !strings.Contains(rendered, "turn3") {
		t.Fatalf("expected turn3 in output, got %q", rendered)
	}
}

func TestFilterLastNTurnsAllTurns(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"turn_end"}
`)
	// Requesting more turns than exist should return all data.
	result := filterLastNTurns(input, 10)
	if len(result) != len(input) {
		t.Fatalf("expected all data returned, got %d vs %d bytes", len(result), len(input))
	}
}

func TestFormatToolCallBash(t *testing.T) {
	args := map[string]interface{}{"command": "echo hello"}
	result := formatToolCall("bash", args, 120)
	if result != "🔧 bash: echo hello" {
		t.Fatalf("expected bash tool call, got %q", result)
	}
}

func TestFormatToolCallBashMultiline(t *testing.T) {
	args := map[string]interface{}{"command": "echo hello\necho world"}
	result := formatToolCall("bash", args, 120)
	if strings.Contains(result, "\n") {
		t.Fatalf("expected newlines replaced in bash command, got %q", result)
	}
	if !strings.Contains(result, "echo hello echo world") {
		t.Fatalf("expected multiline command joined, got %q", result)
	}
}

func TestFormatToolCallRead(t *testing.T) {
	args := map[string]interface{}{"path": "/foo/bar.go"}
	result := formatToolCall("read", args, 120)
	if result != "🔧 read: /foo/bar.go" {
		t.Fatalf("expected read tool call, got %q", result)
	}
}

func TestFormatToolCallTodo(t *testing.T) {
	args := map[string]interface{}{"action": "create", "title": "Fix the bug"}
	result := formatToolCall("todo", args, 120)
	if !strings.Contains(result, "todo create") {
		t.Fatalf("expected todo create, got %q", result)
	}
	if !strings.Contains(result, "Fix the bug") {
		t.Fatalf("expected todo title, got %q", result)
	}
}

func TestFormatToolCallSendToSession(t *testing.T) {
	args := map[string]interface{}{"sessionName": "my-session", "message": "hello"}
	result := formatToolCall("send_to_session", args, 120)
	if !strings.Contains(result, "→ my-session") {
		t.Fatalf("expected session name, got %q", result)
	}
}

func TestFormatToolCallAskUserQuestion(t *testing.T) {
	args := map[string]interface{}{"question": "What should I do?"}
	result := formatToolCall("AskUserQuestion", args, 120)
	if !strings.Contains(result, "What should I do?") {
		t.Fatalf("expected question, got %q", result)
	}
}

func TestFormatToolCallUnknownTool(t *testing.T) {
	args := map[string]interface{}{"foo": "bar"}
	result := formatToolCall("generate_icon", args, 120)
	if result != "🔧 generate_icon" {
		t.Fatalf("expected plain tool name, got %q", result)
	}
}

func TestFormatToolResultNormal(t *testing.T) {
	result := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "hello world"},
		},
	}
	output := formatToolResult(result, 200)
	if output != "→ hello world\n" {
		t.Fatalf("expected formatted result, got %q", output)
	}
}

func TestFormatToolResultTruncation(t *testing.T) {
	longText := strings.Repeat("z", 300)
	result := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": longText},
		},
	}
	output := formatToolResult(result, 200)
	if !strings.Contains(output, "...") {
		t.Fatalf("expected truncation, got len=%d", len(output))
	}
}

func TestFormatToolResultNil(t *testing.T) {
	output := formatToolResult(nil, 200)
	if output != "" {
		t.Fatalf("expected empty for nil result, got %q", output)
	}
}

// --- Edge case tests for hardening ---

func TestFilterLastNTurnsEmptyData(t *testing.T) {
	result := filterLastNTurns([]byte{}, 1)
	if len(result) != 0 {
		t.Fatalf("expected empty result for empty data, got %d bytes", len(result))
	}
}

func TestFilterLastNTurnsNoTurnStart(t *testing.T) {
	input := []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}
{"type":"turn_end"}
`)
	result := filterLastNTurns(input, 1)
	if len(result) != len(input) {
		t.Fatalf("expected all data when no turn_start found, got %d vs %d", len(result), len(input))
	}
}

func TestFilterLastNTurnsNoTrailingNewline(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"turn_end"}`)
	result := filterLastNTurns(input, 1)
	rendered := renderJSONLog(result)
	// Should contain only one turn separator
	if strings.Count(rendered, "---") != 1 {
		t.Fatalf("expected 1 turn separator in filtered output, got %d in %q", strings.Count(rendered, "---"), rendered)
	}
}

func TestFilterLastNTurnsTwo(t *testing.T) {
	input := []byte(`{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"A"}}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"B"}}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"C"}}
{"type":"turn_end"}
`)
	result := filterLastNTurns(input, 2)
	rendered := renderJSONLog(result)
	if strings.Contains(rendered, "A") {
		t.Fatalf("expected turn A filtered out, got %q", rendered)
	}
	if !strings.Contains(rendered, "B") || !strings.Contains(rendered, "C") {
		t.Fatalf("expected turns B and C, got %q", rendered)
	}
}

func TestRenderJSONLogSummaryFlushesTextOnTurnEnd(t *testing.T) {
	// text_delta without text_end — should still flush at turn_end
	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"partial output"}}
{"type":"turn_end"}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "partial output") {
		t.Fatalf("expected flushed text at turn_end, got %q", result)
	}
}

func TestRenderJSONLogSummaryFlushesTextAtEndOfStream(t *testing.T) {
	// text_delta at end of stream with no turn_end or text_end
	input := `{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"dangling text"}}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "dangling text") {
		t.Fatalf("expected flushed dangling text at end of stream, got %q", result)
	}
}

func TestRenderJSONLogSummaryEmptyUserMessage(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":""}]}}
`
	result := renderJSONLogSummary([]byte(input))
	if strings.Contains(result, ">") {
		t.Fatalf("expected no user message prefix for empty text, got %q", result)
	}
}

func TestRenderJSONLogSummaryMultipleTurns(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Hello"}]}}
{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Hi"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Hi"}}
{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":100,"cost":{"total":0.001}}}}
{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Thanks"}]}}
{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Welcome"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"Welcome"}}
{"type":"turn_end","message":{"role":"assistant","usage":{"totalTokens":200,"cost":{"total":0.002}}}}
`
	result := renderJSONLogSummary([]byte(input))
	if !strings.Contains(result, "> Hello") {
		t.Fatalf("expected first user message, got %q", result)
	}
	if !strings.Contains(result, "> Thanks") {
		t.Fatalf("expected second user message, got %q", result)
	}
	if !strings.Contains(result, "turn 1") || !strings.Contains(result, "turn 2") {
		t.Fatalf("expected turn numbers, got %q", result)
	}
}

func TestRenderJSONLineForDumpEmptyLine(t *testing.T) {
	result := renderJSONLineForDump("")
	if result != "" {
		t.Fatalf("expected empty for empty line, got %q", result)
	}
}

func TestRenderJSONLineForDumpInvalidJSON(t *testing.T) {
	result := renderJSONLineForDump("not json at all")
	if result != "" {
		t.Fatalf("expected empty for invalid JSON, got %q", result)
	}
}

func TestRenderJSONLineForDumpToolErrorWithMessage(t *testing.T) {
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[{"type":"text","text":"file not found"}]},"isError":true}`
	result := renderJSONLineForDump(input)
	if !strings.Contains(result, "❌") {
		t.Fatalf("expected error indicator, got %q", result)
	}
	if !strings.Contains(result, "file not found") {
		t.Fatalf("expected error message, got %q", result)
	}
}

func TestRenderJSONLineForDumpToolErrorNoContent(t *testing.T) {
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[]},"isError":true}`
	result := renderJSONLineForDump(input)
	if result != "❌ error\n" {
		t.Fatalf("expected generic error for empty content, got %q", result)
	}
}

func TestRenderJSONLineForDumpTurnEndNoUsage(t *testing.T) {
	input := `{"type":"turn_end"}`
	result := renderJSONLineForDump(input)
	if result != "\n---\n" {
		t.Fatalf("expected plain turn separator, got %q", result)
	}
}

func TestRenderJSONLineForDumpUserMessage(t *testing.T) {
	input := `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Do it"}]}}`
	result := renderJSONLineForDump(input)
	if !strings.Contains(result, "> Do it") {
		t.Fatalf("expected user message, got %q", result)
	}
}

func TestRenderJSONLineForDumpToolStart(t *testing.T) {
	input := `{"type":"tool_execution_start","toolCallId":"abc","toolName":"todo","args":{"action":"list"}}`
	result := renderJSONLineForDump(input)
	if !strings.Contains(result, "🔧 todo list") {
		t.Fatalf("expected todo tool call, got %q", result)
	}
}

func TestRenderJSONLogToolErrorWithoutContent(t *testing.T) {
	input := `{"type":"tool_execution_end","toolCallId":"abc","toolName":"bash","result":{"content":[]},"isError":true}
`
	result := renderJSONLog([]byte(input))
	if !strings.Contains(result, "❌ error") {
		t.Fatalf("expected generic error fallback, got %q", result)
	}
}

func TestFormatToolCallNilArgs(t *testing.T) {
	result := formatToolCall("bash", nil, 120)
	if result != "🔧 bash" {
		t.Fatalf("expected plain tool name for nil args, got %q", result)
	}
}

func TestFormatToolCallEmptyArgs(t *testing.T) {
	result := formatToolCall("bash", map[string]interface{}{}, 120)
	if result != "🔧 bash" {
		t.Fatalf("expected plain tool name for empty args, got %q", result)
	}
}

func TestFormatToolCallBashVeryLong(t *testing.T) {
	longCmd := strings.Repeat("a", 200)
	args := map[string]interface{}{"command": longCmd}
	result := formatToolCall("bash", args, 120)
	if len(result) > 140 { // 🔧 bash: (10 chars) + 120 max
		t.Fatalf("expected truncated command, got len=%d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Fatalf("expected ... suffix, got %q", result)
	}
}

func TestFormatToolCallEdit(t *testing.T) {
	args := map[string]interface{}{"path": "/src/main.go"}
	result := formatToolCall("edit", args, 120)
	if result != "🔧 edit: /src/main.go" {
		t.Fatalf("expected edit with path, got %q", result)
	}
}

func TestFormatToolCallTodoWithId(t *testing.T) {
	args := map[string]interface{}{"action": "get", "id": "TODO-abc123"}
	result := formatToolCall("todo", args, 120)
	if !strings.Contains(result, "todo get") {
		t.Fatalf("expected todo get, got %q", result)
	}
	if !strings.Contains(result, "TODO-abc123") {
		t.Fatalf("expected todo id, got %q", result)
	}
}

func TestFormatToolCallSendToSessionById(t *testing.T) {
	args := map[string]interface{}{"sessionId": "abc-123-def"}
	result := formatToolCall("send_to_session", args, 120)
	if !strings.Contains(result, "→ abc-123-def") {
		t.Fatalf("expected session id, got %q", result)
	}
}

func TestFormatToolResultEmptyText(t *testing.T) {
	result := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": ""},
		},
	}
	output := formatToolResult(result, 200)
	if output != "" {
		t.Fatalf("expected empty for empty text, got %q", output)
	}
}

func TestFormatToolResultNoTextItems(t *testing.T) {
	result := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "image", "data": "..."},
		},
	}
	output := formatToolResult(result, 200)
	if output != "" {
		t.Fatalf("expected empty for non-text items, got %q", output)
	}
}

func TestFormatToolResultEmptyContent(t *testing.T) {
	result := map[string]interface{}{
		"content": []interface{}{},
	}
	output := formatToolResult(result, 200)
	if output != "" {
		t.Fatalf("expected empty for empty content array, got %q", output)
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

func TestFormatToolCallBashUTF8(t *testing.T) {
	// Ensure truncation is rune-safe for multi-byte characters
	cmd := strings.Repeat("日本語", 50) // 150 runes, 450 bytes
	args := map[string]interface{}{"command": cmd}
	result := formatToolCall("bash", args, 120)
	// Should not panic and should be valid UTF-8
	if !strings.HasSuffix(result, "...") {
		t.Fatalf("expected ... suffix, got %q", result)
	}
	// Verify it's valid by counting runes
	runes := []rune(result)
	if len(runes) > 130 { // 🔧 bash: + 120 runes max
		t.Fatalf("expected rune-safe truncation, got %d runes", len(runes))
	}
}

func TestFormatToolResultUTF8(t *testing.T) {
	longText := strings.Repeat("こんにちは", 100)
	result := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": longText},
		},
	}
	output := formatToolResult(result, 200)
	if !strings.HasSuffix(output, "...\n") {
		t.Fatalf("expected ... suffix, got %q", output)
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	lines := splitLines([]byte(""))
	// Empty byte slice with no newlines: the trailing segment is empty string,
	// but splitLines only appends if start < len(data), so empty input → empty slice.
	if len(lines) != 0 {
		t.Fatalf("expected empty slice for empty input, got %v", lines)
	}
}

func TestSplitLinesTrailingNewline(t *testing.T) {
	lines := splitLines([]byte("foo\nbar\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (trailing newline produces no extra), got %d: %v", len(lines), lines)
	}
}

func TestRenderJSONLogEmptyInput(t *testing.T) {
	result := renderJSONLog([]byte(""))
	if result != "" {
		t.Fatalf("expected empty output for empty input, got %q", result)
	}
}

func TestRenderJSONLogSummaryEmptyInput(t *testing.T) {
	result := renderJSONLogSummary([]byte(""))
	if result != "" {
		t.Fatalf("expected empty output for empty input, got %q", result)
	}
}
