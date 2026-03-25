package render

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestRenderThinkingIsHidden(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"thinking_start"}`))
	r.RenderLine([]byte(`{"type":"thinking_delta","delta":"some internal reasoning"}`))
	r.RenderLine([]byte(`{"type":"thinking_end"}`))

	out := buf.String()
	// Pi TUI doesn't show thinking text — should produce no output.
	if strings.Contains(out, "thinking") {
		t.Fatalf("expected thinking to be hidden, got %q", out)
	}
	if strings.Contains(out, "internal reasoning") {
		t.Fatalf("expected thinking delta text to be hidden, got %q", out)
	}
}

func TestRenderTextDelta(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"Hello "}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"world"}`))

	out := buf.String()
	if !strings.Contains(out, "Hello world") {
		t.Fatalf("expected streamed text, got %q", out)
	}
}

func TestRenderNestedTextDelta(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"nested text"}}`))

	out := buf.String()
	if !strings.Contains(out, "nested text") {
		t.Fatalf("expected nested text delta, got %q", out)
	}
}

func TestRenderBashDirectFormat(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"ls -la"}}`))

	out := buf.String()
	// Pi TUI shows "$ command" directly, no "▶ bash" header.
	if !strings.Contains(out, "$ ls -la") {
		t.Fatalf("expected '$ command' format, got %q", out)
	}
	if strings.Contains(out, "▶") {
		t.Fatalf("expected no tool icon for bash, got %q", out)
	}
}

func TestRenderReadFormat(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"/tmp/test.go"}}`))

	out := buf.String()
	if !strings.Contains(out, "Read /tmp/test.go") {
		t.Fatalf("expected 'Read path' format, got %q", out)
	}
}

func TestRenderToolError(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"false"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":true,"result":{"content":[{"type":"text","text":"command not found"}]}}`))

	out := buf.String()
	if !strings.Contains(out, "✗") {
		t.Fatalf("expected error indicator, got %q", out)
	}
	if !strings.Contains(out, "command not found") {
		t.Fatalf("expected error text, got %q", out)
	}
}

func TestRenderToolResultTruncation(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Build a result with many lines — should show only last N with "earlier lines" hint.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "line-"+string(rune('A'+i)))
	}
	text := strings.Join(lines, "\n")

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"test.go"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"` + strings.ReplaceAll(text, "\n", `\n`) + `"}]}}`))

	out := buf.String()
	if !strings.Contains(out, "earlier lines") {
		t.Fatalf("expected truncation hint for long results, got %q", out)
	}
	// Last line should be present.
	if !strings.Contains(out, "line-T") {
		t.Fatalf("expected last line to be shown, got %q", out)
	}
	// First line should NOT be present (it was truncated).
	if strings.Contains(out, "line-A") {
		t.Fatalf("expected first line to be truncated, got %q", out)
	}
}

func TestRenderToolResultShortNotTruncated(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"test.go"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"line one\nline two\nline three"}]}}`))

	out := buf.String()
	if strings.Contains(out, "earlier") {
		t.Fatalf("expected no truncation for short results, got %q", out)
	}
	if !strings.Contains(out, "line one") || !strings.Contains(out, "line three") {
		t.Fatalf("expected all lines shown, got %q", out)
	}
}

func TestRenderTurnEnd(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{"usage":{"totalTokens":5000,"cost":{"total":0.025}}}}`))

	out := buf.String()
	if !strings.Contains(out, "turn 1") {
		t.Fatalf("expected turn number, got %q", out)
	}
	if !strings.Contains(out, "5000 tokens") {
		t.Fatalf("expected token count, got %q", out)
	}
	if !strings.Contains(out, "$0.0250") {
		t.Fatalf("expected cost, got %q", out)
	}
}

func TestRenderUserPrompt(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Fix the bug"}]}}`))

	out := buf.String()
	if !strings.Contains(out, "Fix the bug") {
		t.Fatalf("expected user prompt, got %q", out)
	}
}

func TestRenderAPIError(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"message_start","message":{"role":"assistant","stopReason":"error","errorMessage":"rate limit exceeded"}}`))

	out := buf.String()
	if !strings.Contains(out, "✗") {
		t.Fatalf("expected error indicator, got %q", out)
	}
	if !strings.Contains(out, "rate limit exceeded") {
		t.Fatalf("expected error message, got %q", out)
	}
}

func TestRenderIgnoresNonJSON(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte("this is plain text\n"))
	r.RenderLine([]byte(""))
	r.RenderLine([]byte("   "))

	if buf.Len() != 0 {
		t.Fatalf("expected no output for non-JSON, got %q", buf.String())
	}
}

func TestRenderThinkingToText(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"thinking_start"}`))
	r.RenderLine([]byte(`{"type":"thinking_delta","delta":"internal reasoning"}`))
	r.RenderLine([]byte(`{"type":"thinking_end"}`))
	r.RenderLine([]byte(`{"type":"text_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"After thinking"}`))

	out := buf.String()
	if strings.Contains(out, "internal reasoning") {
		t.Fatalf("expected thinking delta to be hidden, got %q", out)
	}
	if !strings.Contains(out, "After thinking") {
		t.Fatalf("expected text after thinking, got %q", out)
	}
}

func TestRenderToolPartialDelta(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Simulate accumulated partial results (each contains full output so far).
	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"run-test"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_update","toolCallId":"tc1","partialResult":{"content":[{"type":"text","text":"alpha\n"}]}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_update","toolCallId":"tc1","partialResult":{"content":[{"type":"text","text":"alpha\nbeta\n"}]}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"alpha\nbeta\n"}]}}`))

	out := buf.String()
	// Both lines should appear exactly once in the truncated output.
	if count := strings.Count(out, "alpha"); count != 1 {
		t.Fatalf("expected 'alpha' exactly once, got %d times in %q", count, out)
	}
	if count := strings.Count(out, "beta"); count != 1 {
		t.Fatalf("expected 'beta' exactly once, got %d times in %q", count, out)
	}
}

func TestRenderToolPartialTruncated(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Simulate a tool with many lines of partial output.
	var accumulated strings.Builder
	for i := 0; i < 20; i++ {
		accumulated.WriteString(fmt.Sprintf("line-%d\n", i))
	}
	fullText := accumulated.String()

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"seq 20"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_update","toolCallId":"tc1","partialResult":{"content":[{"type":"text","text":"` + strings.ReplaceAll(fullText, "\n", `\n`) + `"}]}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"` + strings.ReplaceAll(fullText, "\n", `\n`) + `"}]}}`))

	out := buf.String()
	// Should be truncated with "earlier lines" hint.
	if !strings.Contains(out, "earlier lines") {
		t.Fatalf("expected truncation for long partial output, got %q", out)
	}
	// Last line should be visible.
	if !strings.Contains(out, "line-19") {
		t.Fatalf("expected last line visible, got %q", out)
	}
	// First line should be truncated.
	if strings.Contains(out, "line-0") {
		t.Fatalf("expected first line truncated, got %q", out)
	}
}

func TestRenderToolEndSkippedWhenPartialsShown(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"run-cmd"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_update","toolCallId":"tc1","partialResult":{"content":[{"type":"text","text":"output-xyz\n"}]}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"output-xyz\n"}]}}`))

	out := buf.String()
	// "output-xyz" should appear exactly once (buffered from partial, rendered at end).
	if count := strings.Count(out, "output-xyz"); count != 1 {
		t.Fatalf("expected 'output-xyz' exactly once (from truncated display), got %d times in %q", count, out)
	}
}

func TestRenderToolEndShownWithoutPartials(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// read tool has no partials, just the end result.
	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"test.go"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"package main\nfunc main() {}"}]}}`))

	out := buf.String()
	if !strings.Contains(out, "package main") {
		t.Fatalf("expected tool result when no partials, got %q", out)
	}
}

func TestRenderBashShowsTookTime(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"echo ok"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"ok\n"}]}}`))

	out := buf.String()
	if !strings.Contains(out, "Took") {
		t.Fatalf("expected 'Took' time for bash, got %q", out)
	}
}

func TestRenderReadNoTookTime(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"read","args":{"path":"test.go"}}`))
	r.RenderLine([]byte(`{"type":"tool_execution_end","isError":false,"result":{"content":[{"type":"text","text":"hello"}]}}`))

	out := buf.String()
	if strings.Contains(out, "Took") {
		t.Fatalf("expected no 'Took' time for read, got %q", out)
	}
}

func TestRenderColorOutput(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled (default)

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"echo hello"}}`))

	out := buf.String()
	if !strings.Contains(out, "\033[") {
		t.Fatalf("expected ANSI escape codes with colors enabled, got %q", out)
	}
}

func TestRenderNoColorOutput(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"echo hello"}}`))

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Fatalf("expected no ANSI escape codes with colors disabled, got %q", out)
	}
}

func TestRenderMultipleTurns(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"First turn"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{"usage":{"totalTokens":1000}}}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"Second turn"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{"usage":{"totalTokens":2000}}}`))

	out := buf.String()
	if !strings.Contains(out, "turn 1") {
		t.Fatalf("expected turn 1, got %q", out)
	}
	if !strings.Contains(out, "turn 2") {
		t.Fatalf("expected turn 2, got %q", out)
	}
}

func TestRenderMultiLineBashCommand(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"tool_execution_start","toolName":"bash","args":{"command":"echo line1\necho line2\necho line3"}}`))

	out := buf.String()
	if !strings.Contains(out, "$ echo line1") {
		t.Fatalf("expected first line with $, got %q", out)
	}
	if !strings.Contains(out, "    echo line2") {
		t.Fatalf("expected continuation lines indented, got %q", out)
	}
}
