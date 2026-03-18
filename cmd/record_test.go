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
	if strings.Contains(logged, "plain stderr line") {
		t.Fatalf("expected non-JSON lines to be filtered from log, got %q", logged)
	}
}

func TestRecordStreamFiltersTerminalEscapeSequences(t *testing.T) {
	// Simulate the OSC notification that pi emits on stderr.
	oscLine := "\x1b]777;notify;\xcf\x80;Agent finished with a very long summary...\n"
	jsonLine := `{"type":"turn_end","message":{"role":"assistant"}}` + "\n"

	input := jsonLine + oscLine

	var stdout bytes.Buffer
	var log bytes.Buffer
	if err := recordStream(strings.NewReader(input), &stdout, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	// stdout should have everything (passthrough to terminal)
	if stdout.String() != input {
		t.Fatalf("expected stdout passthrough, got %q", stdout.String())
	}

	// Log should only have the JSON line
	logged := log.String()
	if !strings.Contains(logged, `"turn_end"`) {
		t.Fatalf("expected JSON line in log, got %q", logged)
	}
	if strings.Contains(logged, "notify") {
		t.Fatalf("expected OSC escape sequence to be filtered from log, got %q", logged)
	}
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`{"type":"test"}`, true},
		{`  {"type":"test"}`, true},
		{"\t{}\n", true},
		{`plain text`, false},
		{"\x1b]777;notify;test\n", false},
		{"", false},
		{"  \n", false},
	}
	for _, tt := range tests {
		got := looksLikeJSON([]byte(tt.input))
		if got != tt.expected {
			t.Errorf("looksLikeJSON(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
