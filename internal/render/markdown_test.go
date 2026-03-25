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
		input   string
		isFence bool
		infoStr string
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
		input string
		level int
		text  string
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

func TestParseBlockquote(t *testing.T) {
	tests := []struct {
		input   string
		isQuote bool
		text    string
	}{
		{"> some text", true, "some text"},
		{">", true, ""},
		{"not a quote", false, ""},
		{">no space", false, ""},
	}
	for _, tt := range tests {
		gotQuote, gotText := parseBlockquote(tt.input)
		if gotQuote != tt.isQuote || gotText != tt.text {
			t.Errorf("parseBlockquote(%q) = (%v, %q), want (%v, %q)", tt.input, gotQuote, gotText, tt.isQuote, tt.text)
		}
	}
}

func TestParseBulletItem(t *testing.T) {
	tests := []struct {
		input    string
		isBullet bool
		indent   int
		text     string
	}{
		{"- item", true, 0, "item"},
		{"* item", true, 0, "item"},
		{"+ item", true, 0, "item"},
		{"  - nested", true, 2, "nested"},
		{"    - deep", true, 4, "deep"},
		{"not a bullet", false, 0, ""},
		{"-no space", false, 0, ""},
	}
	for _, tt := range tests {
		gotBullet, gotIndent, gotText := parseBulletItem(tt.input)
		if gotBullet != tt.isBullet || gotIndent != tt.indent || gotText != tt.text {
			t.Errorf("parseBulletItem(%q) = (%v, %d, %q), want (%v, %d, %q)",
				tt.input, gotBullet, gotIndent, gotText, tt.isBullet, tt.indent, tt.text)
		}
	}
}

func TestParseLink(t *testing.T) {
	tests := []struct {
		input    string
		text     string
		url      string
		consumed int
	}{
		{"[click](http://example.com)", "click", "http://example.com", 27},
		{"[text](url) rest", "text", "url", 11},
		{"[no close paren", "", "", 0},
		{"not a link", "", "", 0},
		{"[]() empty", "", "", 4},
	}
	for _, tt := range tests {
		gotText, gotURL, gotConsumed := parseLink(tt.input)
		if gotText != tt.text || gotURL != tt.url || gotConsumed != tt.consumed {
			t.Errorf("parseLink(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tt.input, gotText, gotURL, gotConsumed, tt.text, tt.url, tt.consumed)
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
		{"*italic* text", "italic text"},
		{"[link](http://x.com)", "link (http://x.com)"},
		{"**bold with `code` inside**", "bold with code inside"},
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
	if strings.Contains(out, "```") {
		t.Fatalf("expected code fences removed, got %q", out)
	}
	if !strings.Contains(out, "After code") {
		t.Fatalf("expected text after code block, got %q", out)
	}
}

func TestRenderCodeBlockLanguageLabel(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```python\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"print('hello')\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```\\n\"}"))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "python") {
		t.Fatalf("expected language label 'python', got %q", out)
	}
	if !strings.Contains(out, "╭─") {
		t.Fatalf("expected code block header border, got %q", out)
	}
}

func TestRenderCodeBlockNoLabel(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"some code\\n\"}"))
	r.RenderLine([]byte("{\"type\":\"text_delta\",\"delta\":\"```\\n\"}"))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	// No language = no label line.
	if strings.Contains(out, "╭─") {
		t.Fatalf("expected no label for unlabeled code block, got %q", out)
	}
	if !strings.Contains(out, "    some code") {
		t.Fatalf("expected indented code, got %q", out)
	}
}

func TestRenderBlockquote(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"> This is a quote.\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"Normal text.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "│") {
		t.Fatalf("expected vertical bar for blockquote, got %q", out)
	}
	if !strings.Contains(out, "This is a quote.") {
		t.Fatalf("expected quote content, got %q", out)
	}
	if strings.Contains(out, "> This") {
		t.Fatalf("expected > prefix removed, got %q", out)
	}
}

func TestRenderBulletList(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"- First item\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"- Second item\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"  - Nested item\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "•") {
		t.Fatalf("expected • bullet character, got %q", out)
	}
	if strings.Contains(out, "- First") {
		t.Fatalf("expected raw - to be replaced with •, got %q", out)
	}
	// Check nesting — nested item should have extra indentation.
	lines := strings.Split(out, "\n")
	foundNested := false
	for _, line := range lines {
		if strings.Contains(line, "Nested") && strings.Contains(line, "  •") {
			foundNested = true
		}
	}
	if !foundNested {
		t.Fatalf("expected nested bullet with extra indent, got %q", out)
	}
}

func TestRenderBoldText(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"This is **important** text.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "\033[1m"+"important") {
		t.Fatalf("expected bold ANSI styling, got %q", out)
	}
	if strings.Contains(out, "**") {
		t.Fatalf("expected ** removed, got %q", out)
	}
}

func TestRenderItalicText(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"This is *emphasized* text.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "\033[3m"+"emphasized") {
		t.Fatalf("expected italic ANSI styling, got %q", out)
	}
	if strings.Contains(out, "*emphasized*") {
		t.Fatalf("expected *text* removed, got %q", out)
	}
}

func TestRenderInlineCode(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"Use ` + "`fmt.Println`" + ` for output.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "\033[36m"+"fmt.Println") {
		t.Fatalf("expected cyan styling for inline code, got %q", out)
	}
}

func TestRenderLink(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"See [docs](https://example.com) for details.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	// Link text should be blue + underline.
	if !strings.Contains(out, "\033[34m\033[4m"+"docs") {
		t.Fatalf("expected blue underlined link text, got %q", out)
	}
	// URL should be dim/gray.
	if !strings.Contains(out, "https://example.com") {
		t.Fatalf("expected URL in output, got %q", out)
	}
	// Raw markdown should be gone.
	if strings.Contains(out, "[docs]") {
		t.Fatalf("expected raw markdown link removed, got %q", out)
	}
}

func TestRenderLinkNoColor(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, WithNoColor())

	r.RenderLine([]byte(`{"type":"text_delta","delta":"See [docs](https://example.com) for details.\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	if !strings.Contains(out, "docs (https://example.com)") {
		t.Fatalf("expected clean link in no-color mode, got %q", out)
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

func TestRenderHeaderLevels(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf) // colors enabled

	r.RenderLine([]byte(`{"type":"text_delta","delta":"# H1 Title\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"## H2 Title\n"}`))
	r.RenderLine([]byte(`{"type":"text_delta","delta":"### H3 Title\n"}`))
	r.RenderLine([]byte(`{"type":"turn_start"}`))
	r.RenderLine([]byte(`{"type":"turn_end","message":{}}`))

	out := buf.String()
	// H1 should have underline.
	if !strings.Contains(out, "\033[1m\033[4m"+"H1 Title") {
		t.Fatalf("expected bold+underline H1, got %q", out)
	}
	// H2 should have bold only.
	if !strings.Contains(out, "\033[1m"+"H2 Title") {
		t.Fatalf("expected bold H2, got %q", out)
	}
	// H3 should have bold+dim.
	if !strings.Contains(out, "\033[1m\033[2m"+"H3 Title") {
		t.Fatalf("expected bold+dim H3, got %q", out)
	}
}
