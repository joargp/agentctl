package render

import (
	"fmt"
	"regexp"
	"strings"
)

// Markdown line-level rendering for assistant text.
// Handles elements that can be detected on a per-line basis:
// - Horizontal rules (---, ***, ___)
// - ATX headers (# ... ######) with level-based coloring
// - Fenced code blocks (``` ... ```) with language labels
// - Blockquotes (> text) with vertical bar
// - Bullet lists (- / * / +) with • character
// - Bold text (**...**)
// - Italic text (*...*)
// - Inline code (`...`)
// - Links [text](url)

const hrWidth = 80

// isHorizontalRule checks if a line is a markdown horizontal rule.
// Must be 3+ of the same character (-, *, _) with optional spaces.
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
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
	if trimmed[level] != ' ' {
		return 0, ""
	}
	text := strings.TrimSpace(trimmed[level+1:])
	// Strip optional trailing hashes: ## Header ##
	text = strings.TrimRight(text, "# ")
	return level, text
}

// parseBlockquote checks if a line starts with > (blockquote).
// Returns the remaining text after stripping the > prefix.
func parseBlockquote(line string) (bool, string) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "> ") {
		return true, trimmed[2:]
	}
	if trimmed == ">" {
		return true, ""
	}
	return false, ""
}

// bulletRe matches unordered list items: optional leading whitespace, then - or * or +, then space.
var bulletRe = regexp.MustCompile(`^(\s*)([-*+])\s+(.*)$`)

// parseBulletItem checks if a line is an unordered list item.
// Returns the indentation level (number of leading spaces), and the item text.
func parseBulletItem(line string) (bool, int, string) {
	m := bulletRe.FindStringSubmatch(line)
	if m == nil {
		return false, 0, ""
	}
	indent := len(m[1])
	text := m[3]
	return true, indent, text
}

// headerStyle returns the ANSI style for a given header level.
// H1: bold + underline, H2: bold, H3+: bold + dim.
func headerStyle(level int) string {
	switch level {
	case 1:
		return bold + underline
	case 2:
		return bold
	default:
		return bold + dim
	}
}

// renderInlineMarkdown renders bold, italic, inline code, and links
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
				out.WriteString(stripInlineMarkdown(line[i+2 : i+2+end]))
				i = i + 2 + end + 2
				continue
			}
		}
		// Italic: *text* (single asterisk, not followed by another)
		if line[i] == '*' && (i+1 >= len(line) || line[i+1] != '*') {
			end := strings.IndexByte(line[i+1:], '*')
			if end >= 0 && (i+1+end+1 >= len(line) || line[i+1+end+1] != '*') {
				out.WriteString(line[i+1 : i+1+end])
				i = i + 1 + end + 1
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
		// Links: [text](url)
		if line[i] == '[' {
			linkText, url, consumed := parseLink(line[i:])
			if consumed > 0 {
				out.WriteString(linkText)
				if url != "" {
					out.WriteString(" (" + url + ")")
				}
				i += consumed
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

// styleInlineMarkdown applies ANSI styles to bold, italic, inline code, and links.
func (r *StreamRenderer) styleInlineMarkdown(line string) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		// Bold: **text**
		if i+1 < len(line) && line[i] == '*' && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "**")
			if end >= 0 {
				content := line[i+2 : i+2+end]
				out.WriteString(bold + r.styleInlineMarkdown(content) + reset)
				i = i + 2 + end + 2
				continue
			}
		}
		// Italic: *text* (single asterisk)
		if line[i] == '*' && (i+1 >= len(line) || line[i+1] != '*') {
			end := strings.IndexByte(line[i+1:], '*')
			if end >= 0 && (i+1+end+1 >= len(line) || line[i+1+end+1] != '*') {
				content := line[i+1 : i+1+end]
				out.WriteString(italic + content + reset)
				i = i + 1 + end + 1
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
		// Links: [text](url)
		if line[i] == '[' {
			linkText, url, consumed := parseLink(line[i:])
			if consumed > 0 {
				out.WriteString(fgBlue + underline + linkText + reset)
				if url != "" {
					out.WriteString(dim + fgGray + " (" + url + ")" + reset)
				}
				i += consumed
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

// parseLink parses a markdown link [text](url) at the start of s.
// Returns the link text, url, and total characters consumed. Returns 0 consumed if not a link.
func parseLink(s string) (text string, url string, consumed int) {
	if len(s) < 4 || s[0] != '[' {
		return "", "", 0
	}
	closeBracket := strings.IndexByte(s[1:], ']')
	if closeBracket < 0 {
		return "", "", 0
	}
	closeBracket++ // adjust for offset
	text = s[1:closeBracket]
	rest := s[closeBracket+1:]
	if len(rest) == 0 || rest[0] != '(' {
		return "", "", 0
	}
	closeParen := strings.IndexByte(rest[1:], ')')
	if closeParen < 0 {
		return "", "", 0
	}
	url = rest[1 : closeParen+1]
	consumed = closeBracket + 1 + closeParen + 2
	return text, url, consumed
}

// renderBlockquote writes a blockquote line with a vertical bar prefix.
func (r *StreamRenderer) renderBlockquote(text string) {
	styled := r.renderInlineMarkdown(text)
	if r.noColor {
		fmt.Fprintf(r.w, "  │ %s\n", styled)
	} else {
		fmt.Fprintf(r.w, "%s  │%s %s\n", dim+fgCyan, reset, styled)
	}
}

// renderBulletItem writes a bullet list item with a • character.
func (r *StreamRenderer) renderBulletItem(indent int, text string) {
	styled := r.renderInlineMarkdown(text)
	// Convert indent spaces to proportional padding (2 spaces per indent level).
	level := indent / 2
	padding := strings.Repeat("  ", level)
	if r.noColor {
		fmt.Fprintf(r.w, "%s• %s\n", padding, styled)
	} else {
		fmt.Fprintf(r.w, "%s%s•%s %s\n", padding, dim, reset, styled)
	}
}

// renderHeader writes a header with level-appropriate styling.
func (r *StreamRenderer) renderHeader(level int, text string) {
	styled := r.renderInlineMarkdown(text)
	if r.noColor {
		fmt.Fprintf(r.w, "%s\n", styled)
	} else {
		fmt.Fprintf(r.w, "%s%s%s\n", headerStyle(level), styled, reset)
	}
}

// renderCodeFenceOpen writes the opening of a code block with optional language label.
func (r *StreamRenderer) renderCodeFenceOpen(lang string) {
	if lang != "" {
		r.writeStyled(dim+fgGray, "  ╭─ "+lang+"\n")
	}
}
