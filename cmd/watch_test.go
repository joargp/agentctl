package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joargp/agentctl/internal/session"
)

func TestCompletionSummaryLinesUsesAssistantTextOnly(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"Say hello"}]}}`,
		`{"type":"tool_execution_start","toolName":"read","args":{"path":"cmd/watch.go"}}`,
		`{"type":"tool_execution_end","toolName":"read","result":{"content":[{"type":"text","text":"package cmd"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hello there."}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello there."}`,
		`{"type":"turn_end","message":{"usage":{"totalTokens":123,"cost":{"total":0.001}}}}`,
	}, "\n"))

	got := completionSummaryLines(data)
	want := []string{"Hello there."}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected line %d = %q, got %q (all: %#v)", i, want[i], got[i], got)
		}
	}
}

func TestCompletionSummaryLinesPreservesAssistantBlockquoteLines(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"> quoted reply"}`,
		`{"type":"text_end","contentIndex":0,"content":"> quoted reply"}`,
	}, "\n"))

	got := completionSummaryLines(data)
	if len(got) != 1 || got[0] != "> quoted reply" {
		t.Fatalf("expected assistant blockquote line to be preserved, got %#v", got)
	}
}

func TestCompletionSummaryLinesEmptyLog(t *testing.T) {
	got := completionSummaryLines([]byte(""))
	if len(got) != 0 {
		t.Fatalf("expected empty for empty log, got %v", got)
	}
}

func TestCompletionSummaryLinesSkipsToolResults(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"tool_execution_end","toolName":"bash","result":{"content":[{"type":"text","text":"hello world"}]},"isError":false}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"The output was hello."}`,
		`{"type":"text_end","contentIndex":0,"content":"The output was hello."}`,
	}, "\n"))

	got := completionSummaryLines(data)
	// Should only include the assistant text, not the tool result
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d: %#v", len(got), got)
	}
	if got[0] != "The output was hello." {
		t.Fatalf("expected assistant text, got %q", got[0])
	}
}

func TestCompletionSummaryLinesTruncatesTo20Lines(t *testing.T) {
	var lines []string
	lines = append(lines, `{"type":"text_start","contentIndex":0}`)
	// Generate 25 lines of text
	bigText := ""
	for i := 0; i < 25; i++ {
		bigText += fmt.Sprintf("Line %d\n", i)
	}
	lines = append(lines, fmt.Sprintf(`{"type":"text_delta","contentIndex":0,"delta":%q}`, bigText))
	lines = append(lines, fmt.Sprintf(`{"type":"text_end","contentIndex":0,"content":%q}`, bigText))
	data := []byte(strings.Join(lines, "\n"))

	got := completionSummaryLines(data)
	if len(got) > 20 {
		t.Fatalf("expected max 20 lines, got %d", len(got))
	}
}

func TestTruncateTask(t *testing.T) {
	// Single line under limit
	if got := truncateTask("hello", 100); got != "hello" {
		t.Fatalf("expected unchanged short task, got %q", got)
	}
	// Multi-line takes first line only
	if got := truncateTask("line1\nline2\nline3", 100); got != "line1" {
		t.Fatalf("expected first line only, got %q", got)
	}
	// Long first line gets truncated
	long := strings.Repeat("x", 200)
	got := truncateTask(long, 100)
	if len(got) > 100 {
		t.Fatalf("expected truncated to 100, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ... suffix, got %q", got)
	}
	// Whitespace trimmed
	if got := truncateTask("  hello  ", 100); got != "hello" {
		t.Fatalf("expected trimmed, got %q", got)
	}
}

func TestCompletionMessageFallsBackToFullLogWhenTailMissesDelta(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "session.log")

	big := strings.Repeat("A", 600*1024)
	data := strings.Join([]string{
		`{"type":"text_start","contentIndex":0}`,
		fmt.Sprintf(`{"type":"text_delta","contentIndex":0,"delta":%q}`, big),
		fmt.Sprintf(`{"type":"text_end","contentIndex":0,"content":%q}`, big),
	}, "\n")
	if err := os.WriteFile(logFile, []byte(data), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	s := &session.Session{
		ID:      "abc12345",
		Model:   "openai/gpt-5.4",
		Task:    "Write a long response",
		LogFile: logFile,
	}

	msg := completionMessage(s)
	if !strings.Contains(msg, "**Summary:**") {
		t.Fatalf("expected completion message to include summary block, got: %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("A", 64)) {
		t.Fatalf("expected completion message to include assistant text from full-log fallback")
	}
}
