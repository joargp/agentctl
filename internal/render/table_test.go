package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsTableRow(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"| a | b |", true},
		{"|---|---|", true},
		{"| a |", true},
		{"not a table", false},
		{"| only start", false},
		{"only end |", false},
		{"||", true},
		{"", false},
		{"|", false},
	}
	for _, tt := range tests {
		got := isTableRow(tt.input)
		if got != tt.expected {
			t.Errorf("isTableRow(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsSeparatorRow(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"|---|---|", true},
		{"| --- | --- |", true},
		{"|:---:|:---:|", true},
		{"| a | b |", false},
		{"not a row", false},
	}
	for _, tt := range tests {
		got := isSeparatorRow(tt.input)
		if got != tt.expected {
			t.Errorf("isSeparatorRow(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestParseCells(t *testing.T) {
	cells := parseCells("| hello | world |")
	if len(cells) != 2 || cells[0] != "hello" || cells[1] != "world" {
		t.Fatalf("expected [hello, world], got %v", cells)
	}
}

func TestRenderBoxTable(t *testing.T) {
	lines := []string{
		"| Package | Files |",
		"|---|---|",
		"| cmd/ | 23 |",
		"| internal/ | 7 |",
	}
	result := renderBoxTable(lines)

	// Should contain box-drawing characters.
	if !strings.Contains(result, "┌") {
		t.Fatalf("expected top-left corner, got %q", result)
	}
	if !strings.Contains(result, "┘") {
		t.Fatalf("expected bottom-right corner, got %q", result)
	}
	if !strings.Contains(result, "│") {
		t.Fatalf("expected vertical bars, got %q", result)
	}
	// Should contain cell content.
	if !strings.Contains(result, "Package") {
		t.Fatalf("expected header content, got %q", result)
	}
	if !strings.Contains(result, "cmd/") {
		t.Fatalf("expected data content, got %q", result)
	}
	// Separator row should NOT appear as text.
	if strings.Contains(result, "---") {
		t.Fatalf("expected separator row to be replaced, got %q", result)
	}
}

func TestRenderBoxTableAlignment(t *testing.T) {
	lines := []string{
		"| A | B |",
		"|---|---|",
		"| short | x |",
		"| very long cell | y |",
	}
	result := renderBoxTable(lines)

	// All rows should have same width — check that borders align.
	resultLines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(resultLines) < 3 {
		t.Fatalf("expected at least 3 rendered lines, got %d", len(resultLines))
	}
	// Top and bottom borders should be the same length.
	topLen := len(resultLines[0])
	bottomLen := len(resultLines[len(resultLines)-1])
	if topLen != bottomLen {
		t.Fatalf("expected top/bottom borders same length, got %d vs %d", topLen, bottomLen)
	}
}

func TestStreamRendererTableInText(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Simulate streaming a markdown table in text deltas.
	r.RenderLine([]byte(`{"type":"text_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"Here are the results:\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"| Name | Count |\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"|---|---|\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"| foo | 3 |\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"| bar | 7 |\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"\nDone.\n"}`))
	// Flush via turn_end.
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	// Should contain box-drawing table.
	if !strings.Contains(out, "┌") {
		t.Fatalf("expected box-drawing table, got %q", out)
	}
	if !strings.Contains(out, "foo") {
		t.Fatalf("expected cell content 'foo', got %q", out)
	}
	// Should NOT contain raw markdown pipes as table rows.
	if strings.Contains(out, "|---|") {
		t.Fatalf("expected markdown separator to be replaced, got %q", out)
	}
	// Non-table text should still appear.
	if !strings.Contains(out, "Here are the results:") {
		t.Fatalf("expected non-table text, got %q", out)
	}
	if !strings.Contains(out, "Done.") {
		t.Fatalf("expected trailing text, got %q", out)
	}
}

func TestStreamRendererTablePartialDeltas(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Simulate table arriving in partial deltas (split mid-line).
	r.RenderLine([]byte(`{"type":"text_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"| A "}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"| B |\n|---"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"|---|\n| 1 | 2 |\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"end\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "┌") {
		t.Fatalf("expected box-drawing table from partial deltas, got %q", out)
	}
	if !strings.Contains(out, "end") {
		t.Fatalf("expected trailing text after table, got %q", out)
	}
}

func TestStreamRendererNoTablePassthrough(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Regular text without tables should pass through unchanged.
	r.RenderLine([]byte(`{"type":"text_start"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"Hello world\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"No tables here.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if strings.Contains(out, "┌") {
		t.Fatalf("expected no box-drawing for non-table text, got %q", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Fatalf("expected text passthrough, got %q", out)
	}
}
