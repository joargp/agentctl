package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsHorizontalRule(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"---", true},
		{"***", true},
		{"___", true},
		{"----", true},
		{"- - -", true},
		{"* * *", true},
		{"_ _ _", true},
		{"--", false},
		{"text", false},
		{"", false},
		{"--- text", false},
	}
	for _, tt := range tests {
		got := isHorizontalRule(tt.input)
		if got != tt.expected {
			t.Errorf("isHorizontalRule(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsCodeFence(t *testing.T) {
	tests := []struct {
		input     string
		isFence   bool
		infoStr   string
	}{
		{"```", true, ""},
		{"```go", true, "go"},
		{"```python", true, "python"},
		{"~~~", true, ""},
		{"~~~bash", true, "bash"},
		{"`` `", false, ""},
		{"not a fence", false, ""},
		{"", false, ""},
	}
	for _, tt := range tests {
		gotFence, gotInfo := isCodeFence(tt.input)
		if gotFence != tt.isFence || gotInfo != tt.infoStr {
			t.Errorf("isCodeFence(%q) = (%v, %q), want (%v, %q)", tt.input, gotFence, gotInfo, tt.isFence, tt.infoStr)
		}
	}
}

func TestParseATXHeader(t *testing.T) {
	tests := []struct {
		input    string
		level    int
		text     string
	}{
		{"# Title", 1, "Title"},
		{"## Section", 2, "Section"},
		{"### Sub", 3, "Sub"},
		{"###### Deep", 6, "Deep"},
		{"## Title ##", 2, "Title"},
		{"Not a header", 0, ""},
		{"#NoSpace", 0, ""},
		{"", 0, ""},
		{"####### TooDeep", 0, ""},
	}
	for _, tt := range tests {
		gotLevel, gotText := parseATXHeader(tt.input)
		if gotLevel != tt.level || gotText != tt.text {
			t.Errorf("parseATXHeader(%q) = (%d, %q), want (%d, %q)", tt.input, gotLevel, gotText, tt.level, tt.text)
		}
	}
}

func TestStripInlineMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"**bold**", "bold"},
		{"`code`", "code"},
		{"some **bold** text", "some bold text"},
		{"use `fmt.Println`", "use fmt.Println"},
		{"**bold** and `code`", "bold and code"},
		{"no markdown", "no markdown"},
		{"unclosed **bold", "unclosed **bold"},
		{"unclosed `code", "unclosed `code"},
	}
	for _, tt := range tests {
		got := stripInlineMarkdown(tt.input)
		if got != tt.expected {
			t.Errorf("stripInlineMarkdown(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRenderHorizontalRule(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"Before\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"---\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"After\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "────") {
		t.Fatalf("expected horizontal rule rendered as ─, got %q", out)
	}
	if strings.Contains(out, "---") {
		t.Fatalf("expected raw --- to be replaced, got %q", out)
	}
}

func TestRenderATXHeader(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"## My Section\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "My Section") {
		t.Fatalf("expected header text, got %q", out)
	}
	if strings.Contains(out, "##") {
		t.Fatalf("expected ## prefix removed, got %q", out)
	}
}

func TestRenderCodeBlock(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	// Send code block as individual lines via JSON deltas.
	// Each delta ends with \n which the JSON parser will decode.
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```go\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"func main() {}\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"After code\\n\"}"))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "    func main()") {
		t.Fatalf("expected indented code, got %q", out)
	}
	// Fences should not appear in output.
	if strings.Contains(out, "```") {
		t.Fatalf("expected code fences removed, got %q", out)
	}
	if !strings.Contains(out, "After code") {
		t.Fatalf("expected text after code block, got %q", out)
	}
}

func TestRenderBoldText(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"This is **important** text.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "important") {
		t.Fatalf("expected bold content, got %q", out)
	}
	// Should have bold ANSI codes.
	if !strings.Contains(out, "\033[1m"+"important") {
		t.Fatalf("expected bold ANSI styling, got %q", out)
	}
	// Should not contain raw asterisks.
	if strings.Contains(out, "**") {
		t.Fatalf("expected ** removed, got %q", out)
	}
}

func TestRenderInlineCode(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"Use ` + "`fmt.Println`" + ` for output.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "fmt.Println") {
		t.Fatalf("expected inline code content, got %q", out)
	}
	// Should have cyan ANSI codes for inline code.
	if !strings.Contains(out, "\033[36m"+"fmt.Println") {
		t.Fatalf("expected cyan styling for inline code, got %q", out)
	}
}

func TestRenderNoColorStripsMarkdown(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"**bold** and ` + "`code`" + ` text.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if strings.Contains(out, "**") {
		t.Fatalf("expected ** stripped in no-color mode, got %q", out)
	}
	if strings.Contains(out, "`") {
		t.Fatalf("expected backticks stripped in no-color mode, got %q", out)
	}
	if !strings.Contains(out, "bold and code text.") {
		t.Fatalf("expected clean text, got %q", out)
	}
}

func TestRenderHeaderWithInlineMarkdown(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"## The ` + "`stream`" + ` Command\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if strings.Contains(out, "##") {
		t.Fatalf("expected ## removed, got %q", out)
	}
	if strings.Contains(out, "`") {
		t.Fatalf("expected backticks stripped, got %q", out)
	}
	if !strings.Contains(out, "The stream Command") {
		t.Fatalf("expected clean header text, got %q", out)
	}
}
