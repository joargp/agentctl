package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joargp/agentctl/internal/session"
)

func TestCompletionSummaryLinesUsesCondensedSummary(t *testing.T) {
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
	want := []string{
		"🔧 read: cmd/watch.go",
		"→ package cmd",
		"Hello there.",
	}
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
	if !strings.Contains(msg, "**Output (last lines):**") {
		t.Fatalf("expected completion message to include output block, got: %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("A", 64)) {
		t.Fatalf("expected completion message to include assistant text from full-log fallback")
	}
}
