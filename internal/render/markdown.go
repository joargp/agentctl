package render

import (
	"strings"
)

// Markdown line-level rendering for assistant text.
// Handles elements that can be detected on a per-line basis:
// - Horizontal rules (---, ***, ___)
// - ATX headers (# ... ######)
// - Fenced code blocks (``` ... ```)
// - Bold text (**...**)
// - Inline code (`...`)

const hrWidth = 80

// isHorizontalRule checks if a line is a markdown horizontal rule.
// Must be 3+ of the same character (-, *, _) with optional spaces.
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	// Check for ---, ***, ___
	char := trimmed[0]
	if char != '-' && char != '*' && char != '_' {
		return false
	}
	for _, r := range trimmed {
		if r != rune(char) && r != ' ' {
			return false
		}
	}
	return true
}

// isCodeFence checks if a line is a fenced code block delimiter (``` or ~~~).
// Returns true and the info string (e.g., "go" from ```go) if it's a fence.
func isCodeFence(line string) (bool, string) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "```") {
		info := strings.TrimSpace(trimmed[3:])
		return true, info
	}
	if strings.HasPrefix(trimmed, "~~~") {
		info := strings.TrimSpace(trimmed[3:])
		return true, info
	}
	return false, ""
}

// parseATXHeader checks if a line is an ATX header (# to ######).
// Returns the header level (1-6) and the text content, or 0 if not a header.
func parseATXHeader(line string) (int, string) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '#' {
		return 0, ""
	}
	level := 0
	for i := 0; i < len(trimmed) && trimmed[i] == '#'; i++ {
		level++
	}
	if level > 6 || level >= len(trimmed) {
		return 0, ""
	}
	// Must have a space after the hashes (or be just hashes).
	if trimmed[level] != ' ' {
		return 0, ""
	}
	text := strings.TrimSpace(trimmed[level+1:])
	// Strip optional trailing hashes: ## Header ##
	text = strings.TrimRight(text, "# ")
	return level, text
}

// renderInlineMarkdown renders bold (**...**) and inline code (`...`)
// within a single line. Returns the styled string.
func (r *StreamRenderer) renderInlineMarkdown(line string) string {
	if r.noColor {
		return stripInlineMarkdown(line)
	}
	return r.styleInlineMarkdown(line)
}

// stripInlineMarkdown removes markdown syntax but keeps the text.
func stripInlineMarkdown(line string) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		// Bold: **text**
		if i+1 < len(line) && line[i] == '*' && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "**")
			if end >= 0 {
				out.WriteString(line[i+2 : i+2+end])
				i = i + 2 + end + 2
				continue
			}
		}
		// Inline code: `text`
		if line[i] == '`' {
			end := strings.IndexByte(line[i+1:], '`')
			if end >= 0 {
				out.WriteString(line[i+1 : i+1+end])
				i = i + 1 + end + 1
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

// styleInlineMarkdown applies ANSI styles to bold and inline code.
func (r *StreamRenderer) styleInlineMarkdown(line string) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		// Bold: **text**
		if i+1 < len(line) && line[i] == '*' && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "**")
			if end >= 0 {
				content := line[i+2 : i+2+end]
				out.WriteString(bold + content + reset)
				i = i + 2 + end + 2
				continue
			}
		}
		// Inline code: `text`
		if line[i] == '`' {
			end := strings.IndexByte(line[i+1:], '`')
			if end >= 0 {
				content := line[i+1 : i+1+end]
				out.WriteString(fgCyan + content + reset)
				i = i + 1 + end + 1
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}
