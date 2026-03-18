package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecordStreamSanitizesLogButPreservesStdout(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":" world","partial":"Hello world","message":{"role":"assistant","content":[]}}}`,
		`plain stderr line`,
		"",
	}, "\n")

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	if stdout.String() != input {
		t.Fatalf("expected stdout passthrough, got %q", stdout.String())
	}

	logged := log.String()
	if strings.Contains(logged, `"partial"`) {
		t.Fatalf("expected sanitized log to drop partial field, got %q", logged)
	}
	if strings.Contains(logged, `"message"`) {
		t.Fatalf("expected sanitized log to drop message field, got %q", logged)
	}
	if !strings.Contains(logged, `"delta":" world"`) {
		t.Fatalf("expected sanitized log to keep delta, got %q", logged)
	}
	if !strings.Contains(logged, "plain stderr line") {
		t.Fatalf("expected non-JSON lines to be preserved, got %q", logged)
	}
}
