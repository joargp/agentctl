// Package render provides a streaming renderer that converts pi JSON events
// into colored, human-readable terminal output resembling the standard Pi TUI.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ANSI escape sequences for Pi-like terminal styling.
const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"

	// Foreground colors
	fgRed    = "\033[31m"
	fgYellow = "\033[33m"
	fgBlue   = "\033[34m"
	fgCyan   = "\033[36m"
	fgGray   = "\033[90m"
)

// maxToolOutputLines controls how many trailing lines of tool output to show.
// Earlier lines are collapsed with a "... (N earlier lines)" hint, matching
// the Pi TUI's default behavior.
const maxToolOutputLines = 5

// StreamRenderer processes individual JSON events and writes formatted
// terminal output. It maintains minimal state to handle streaming deltas
// and produce coherent output across event boundaries.
type StreamRenderer struct {
	w              io.Writer
	noColor        bool
	state          renderState
	turnCount      int
	hadToolPartial bool      // true if tool_execution_update emitted partial results
	lastPartialLen int       // length of last accumulated partial result (for delta computation)
	lastPartialID  string    // toolCallId of the current partial sequence
	toolLines      []string  // accumulated lines during tool streaming (for tail truncation)
	toolName       string    // name of the currently executing tool
	toolStartTime  time.Time // when the current tool started
	needsNewline   bool      // true if assistant text was written without a trailing newline
}

type renderState int

const (
	stateIdle renderState = iota
	stateThinking
	stateWriting
	stateTool
)

// Option configures a StreamRenderer.
type Option func(*StreamRenderer)

// WithNoColor disables ANSI color output.
func WithNoColor() Option {
	return func(r *StreamRenderer) { r.noColor = true }
}

// New creates a StreamRenderer that writes formatted output to w.
func New(w io.Writer, opts ...Option) *StreamRenderer {
	r := &StreamRenderer{
		w: w,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RenderLine processes a single NDJSON line and writes formatted output.
// Non-JSON lines are silently ignored.
func (r *StreamRenderer) RenderLine(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || trimmed[0] != '{' {
		return
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return
	}

	r.renderEvent(event)
}

func (r *StreamRenderer) renderEvent(event map[string]interface{}) {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_start":
		r.renderMessageStart(event)
	case "message_update":
		r.renderMessageUpdate(event)
	case "text_delta":
		r.renderTextDelta(event)
	case "text_start":
		r.renderTextStart()
	case "text_end":
		// text_end doesn't need explicit handling in streaming mode
	case "thinking_start":
		r.state = stateThinking
	case "thinking_delta":
		// Pi TUI doesn't show thinking text — silently consume
	case "thinking_end":
		r.state = stateIdle
	case "tool_execution_start":
		r.renderToolStart(event)
	case "tool_execution_update":
		r.renderToolUpdate(event)
	case "tool_execution_end":
		r.renderToolEnd(event)
	case "turn_start":
		r.turnCount++
	case "turn_end":
		r.renderTurnEnd(event)
	}
}

func (r *StreamRenderer) renderMessageStart(event map[string]interface{}) {
	msg, _ := event["message"].(map[string]interface{})
	if msg == nil {
		return
	}

	role, _ := msg["role"].(string)
	if role == "user" {
		content, _ := msg["content"].([]interface{})
		for _, c := range content {
			cm, _ := c.(map[string]interface{})
			if t, _ := cm["type"].(string); t == "text" {
				text, _ := cm["text"].(string)
				if text != "" {
					r.writeUserPrompt(text)
				}
			}
		}
	}

	if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
		r.renderAPIError(msg)
	}
}

func (r *StreamRenderer) renderMessageUpdate(event map[string]interface{}) {
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		return
	}

	aeType, _ := ae["type"].(string)
	switch aeType {
	case "thinking_start":
		r.state = stateThinking
	case "thinking_delta":
		// Pi TUI doesn't show thinking text
	case "thinking_end":
		r.state = stateIdle
	case "text_start":
		r.renderTextStart()
	case "text_delta":
		delta, _ := ae["delta"].(string)
		if delta != "" {
			r.writeAssistantText(delta)
		}
	case "text_end":
		// no-op in streaming mode
	}
}

func (r *StreamRenderer) renderTextDelta(event map[string]interface{}) {
	delta, _ := event["delta"].(string)
	if delta != "" {
		r.writeAssistantText(delta)
	}
}

func (r *StreamRenderer) renderTextStart() {
	r.state = stateWriting
}

func (r *StreamRenderer) renderToolStart(event map[string]interface{}) {
	// Ensure assistant text ends with a newline before tool output.
	if r.needsNewline {
		fmt.Fprint(r.w, "\n")
		r.needsNewline = false
	}

	toolName, _ := event["toolName"].(string)
	args, _ := event["args"].(map[string]interface{})
	r.state = stateTool
	r.hadToolPartial = false
	r.lastPartialLen = 0
	r.lastPartialID = ""
	r.toolLines = nil
	r.toolName = toolName
	r.toolStartTime = time.Now()

	fmt.Fprint(r.w, "\n")
	r.writeToolCall(toolName, args)
}

func (r *StreamRenderer) renderToolUpdate(event map[string]interface{}) {
	partialResult, _ := event["partialResult"].(map[string]interface{})
	if partialResult == nil {
		return
	}
	text := toolResultText(partialResult)
	if text == "" {
		return
	}

	// partialResult contains the ACCUMULATED output so far, not a delta.
	// Compute the new portion by comparing with the last partial we rendered.
	toolCallID, _ := event["toolCallId"].(string)
	if toolCallID != "" && toolCallID == r.lastPartialID && r.lastPartialLen > 0 {
		if r.lastPartialLen < len(text) {
			text = text[r.lastPartialLen:]
		} else {
			return // no new content
		}
	}

	r.lastPartialID = toolCallID
	r.lastPartialLen += len(text)
	r.hadToolPartial = true

	// Buffer lines for tail-truncated display at tool_execution_end.
	newLines := strings.Split(text, "\n")
	for _, line := range newLines {
		if line != "" || len(r.toolLines) > 0 { // skip leading empty
			r.toolLines = append(r.toolLines, line)
		}
	}
}

func (r *StreamRenderer) renderToolEnd(event map[string]interface{}) {
	isError, _ := event["isError"].(bool)
	result, _ := event["result"].(map[string]interface{})
	elapsed := time.Since(r.toolStartTime)

	if isError {
		text := toolResultText(result)
		if text == "" {
			text = "error"
		}
		r.writeStyled(fgRed, "  ✗ "+truncate(singleLine(text), 200)+"\n")
	} else if r.hadToolPartial {
		// We buffered partial output — now display it tail-truncated like Pi TUI.
		r.renderTruncatedLines(r.toolLines)
	} else {
		// No partials (e.g. read, edit, write) — show the final result truncated.
		text := toolResultText(result)
		if text != "" {
			lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
			r.renderTruncatedLines(lines)
		}
	}

	// Show execution time for bash (like Pi's "Took 0.0s").
	if r.toolName == "bash" && !isError {
		r.writeStyled(dim+fgGray, fmt.Sprintf("  Took %.1fs\n", elapsed.Seconds()))
	}
	fmt.Fprint(r.w, "\n")

	r.toolLines = nil
	r.hadToolPartial = false
	r.state = stateIdle
}

// renderTruncatedLines displays lines with Pi-TUI-style tail truncation:
// if there are more than maxToolOutputLines, shows "... (N earlier lines)"
// followed by the last maxToolOutputLines lines.
func (r *StreamRenderer) renderTruncatedLines(lines []string) {
	// Trim trailing empty lines.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return
	}

	if len(lines) > maxToolOutputLines {
		hidden := len(lines) - maxToolOutputLines
		r.writeStyled(dim+fgGray, fmt.Sprintf("  ... (%d earlier lines)\n", hidden))
		lines = lines[hidden:]
	}
	for _, line := range lines {
		r.writeStyled(dim, "  "+line+"\n")
	}
}

func (r *StreamRenderer) renderTurnEnd(event map[string]interface{}) {
	if r.needsNewline {
		fmt.Fprint(r.w, "\n")
		r.needsNewline = false
	}

	msg, _ := event["message"].(map[string]interface{})
	tokens, costStr, hasSummary := usageSummary(msg)

	fmt.Fprint(r.w, "\n")
	if hasSummary {
		r.writeStyled(dim+fgGray, fmt.Sprintf("  ── turn %d · %d tokens%s ──\n", r.turnCount, tokens, costStr))
	} else {
		r.writeStyled(dim+fgGray, fmt.Sprintf("  ── turn %d ──\n", r.turnCount))
	}
	fmt.Fprint(r.w, "\n")
	r.state = stateIdle
}

func (r *StreamRenderer) renderAPIError(msg map[string]interface{}) {
	errMsg, _ := msg["errorMessage"].(string)
	if errMsg == "" {
		errMsg = "API error"
	}
	// Try to extract a readable message from nested JSON error strings.
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(errMsg), &parsed) == nil {
		if inner, ok := parsed["error"].(map[string]interface{}); ok {
			if m, ok := inner["message"].(string); ok {
				errMsg = m
			}
		}
	}
	r.writeStyled(bold+fgRed, "\n  ✗ "+truncate(errMsg, 500)+"\n\n")
}

// --- Output helpers ---

func (r *StreamRenderer) writeUserPrompt(text string) {
	// Pi TUI shows the user prompt as plain text with a blank line after.
	fmt.Fprintf(r.w, "\n %s\n\n", text)
}

func (r *StreamRenderer) writeAssistantText(text string) {
	r.state = stateWriting
	// Ensure a blank line before assistant text after tool output or turn boundary.
	// Stream text with leading space indent to match Pi TUI's layout.
	fmt.Fprint(r.w, text)
	r.needsNewline = len(text) > 0 && text[len(text)-1] != '\n'
}

func (r *StreamRenderer) writeToolCall(name string, args map[string]interface{}) {
	switch name {
	case "bash":
		// Pi TUI: just "$ command" on its own line, no tool name header.
		if cmd, ok := args["command"].(string); ok {
			cmdLines := strings.Split(strings.TrimSpace(cmd), "\n")
			for i, line := range cmdLines {
				if i == 0 {
					r.writeStyled(fgYellow, "  $ "+line+"\n")
				} else {
					r.writeStyled(fgYellow, "    "+line+"\n")
				}
			}
		} else {
			r.writeStyled(fgYellow, "  $ bash\n")
		}
		fmt.Fprint(r.w, "\n")
	case "read":
		if p, ok := args["path"].(string); ok {
			r.writeStyled(fgCyan, "  Read "+p+"\n")
		} else {
			r.writeStyled(fgCyan, "  Read\n")
		}
	case "write":
		if p, ok := args["path"].(string); ok {
			r.writeStyled(fgCyan, "  Write "+p+"\n")
		} else {
			r.writeStyled(fgCyan, "  Write\n")
		}
	case "edit":
		if p, ok := args["path"].(string); ok {
			r.writeStyled(fgCyan, "  Edit "+p+"\n")
		} else {
			r.writeStyled(fgCyan, "  Edit\n")
		}
	case "todo":
		detail := "todo"
		if action, ok := args["action"].(string); ok {
			detail = "todo " + action
			if title, ok := args["title"].(string); ok {
				detail += ": " + title
			}
		}
		r.writeStyled(fgCyan, "  "+detail+"\n")
	case "send_to_session":
		detail := "send_to_session"
		if n, ok := args["sessionName"].(string); ok {
			detail += " → " + n
		}
		r.writeStyled(fgCyan, "  "+detail+"\n")
	case "AskUserQuestion":
		if q, ok := args["question"].(string); ok {
			r.writeStyled(fgCyan, "  ? "+truncate(q, 100)+"\n")
		} else {
			r.writeStyled(fgCyan, "  AskUserQuestion\n")
		}
	case "spawn_agent", "spawn_agents":
		detail := name
		if n, ok := args["name"].(string); ok {
			detail += " " + n
		}
		r.writeStyled(fgCyan, "  "+detail+"\n")
	default:
		r.writeStyled(fgCyan, "  "+name+"\n")
	}
}

func (r *StreamRenderer) writeStyled(style, text string) {
	if r.noColor {
		fmt.Fprint(r.w, text)
	} else {
		fmt.Fprint(r.w, style+text+reset)
	}
}

// --- Utility functions ---

func toolResultText(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	content, _ := result["content"].([]interface{})
	for _, c := range content {
		cm, _ := c.(map[string]interface{})
		if t, _ := cm["type"].(string); t == "text" {
			text, _ := cm["text"].(string)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func usageSummary(msg map[string]interface{}) (int, string, bool) {
	if msg == nil {
		return 0, "", false
	}
	usage, _ := msg["usage"].(map[string]interface{})
	if usage == nil {
		return 0, "", false
	}
	tokens, _ := usage["totalTokens"].(float64)
	if tokens <= 0 {
		return 0, "", false
	}
	costInfo, _ := usage["cost"].(map[string]interface{})
	cost := ""
	if total, ok := costInfo["total"].(float64); ok && total > 0 {
		cost = fmt.Sprintf(" · $%.4f", total)
	}
	return int(tokens), cost, true
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "…"
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
