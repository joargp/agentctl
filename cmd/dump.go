package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/render"
	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:               "dump <id>",
	Short:             "Print the last N lines of a session's output",
	ValidArgsFunction: completeSessionIDs,
	Long: `Reads output from the session's JSON log file and renders human-readable text.
Extracts assistant text, tool calls, and tool results from the NDJSON event stream.

Examples:
  agentctl dump abc123
  agentctl dump abc123 --summary
  agentctl dump abc123 --condensed
  agentctl dump abc123 --summary --last
  agentctl dump abc123 --summary --turns 3
  agentctl dump abc123 --lines 200
  agentctl dump abc123 --json
  agentctl dump abc123 --follow
  agentctl dump abc123 --no-header`,
	Args: cobra.ExactArgs(1),
	RunE: runDump,
}

var (
	dumpLines     int
	dumpJSON      bool
	dumpFollow    bool
	dumpSummary   bool
	dumpCondensed bool
	dumpNoHeader  bool
	dumpTurns     int
	dumpLast      bool
	dumpRender    bool
	dumpNoColor   bool
)

func init() {
	dumpCmd.Flags().IntVarP(&dumpLines, "lines", "n", 100, "number of lines to show")
	dumpCmd.Flags().BoolVar(&dumpJSON, "json", false, "output raw JSON events")
	dumpCmd.Flags().BoolVarP(&dumpFollow, "follow", "f", false, "follow output like tail -f (rendered or raw JSON)")
	dumpCmd.Flags().BoolVar(&dumpSummary, "summary", false, "summary output (tool calls + final text only, no intermediate deltas)")
	dumpCmd.Flags().BoolVar(&dumpCondensed, "condensed", false, "activity timeline output (collapse repeated file tools, hide verbose tool results)")
	dumpCmd.Flags().BoolVar(&dumpNoHeader, "no-header", false, "skip the session metadata header")
	dumpCmd.Flags().IntVarP(&dumpTurns, "turns", "t", 0, "show only the last N turns (0 = all)")
	dumpCmd.Flags().BoolVar(&dumpLast, "last", false, "show only the last turn (shortcut for --turns 1)")
	dumpCmd.Flags().BoolVar(&dumpRender, "render", false, "use Pi-TUI-like formatting with colors and markdown rendering")
	dumpCmd.Flags().BoolVar(&dumpNoColor, "no-color", false, "disable colored output (use with --render)")
	rootCmd.AddCommand(dumpCmd)
}

// printSessionHeader prints a brief session metadata header.
func printSessionHeader(s *session.Session) {
	if dumpNoHeader {
		return
	}
	running := tmux.SessionExists(s.TmuxSession)
	statusLabel := "done"
	if running {
		statusLabel = "running"
	}
	age := time.Since(s.StartedAt).Round(time.Second)
	cost := extractTotalCost(s.LogFile)
	costStr := ""
	if cost > 0 {
		costStr = fmt.Sprintf("  cost=$%.4f", cost)
	}
	task := truncateRunesASCII(singleLineTrimmed(s.Task), 80)
	fmt.Printf("═══ %s  %s  %s  %s%s\n", s.ID, s.Label(), statusLabel, age, costStr)
	fmt.Printf("    %s\n\n", task)
}

func runDump(_ *cobra.Command, args []string) error {
	if dumpSummary && dumpCondensed {
		return fmt.Errorf("--summary and --condensed cannot be used together")
	}
	if dumpFollow && dumpCondensed {
		return fmt.Errorf("--condensed is not supported with --follow")
	}

	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	if dumpFollow {
		if dumpRender {
			return runDumpFollowRendered(s)
		}
		return runDumpFollow(s)
	}

	// Non-follow render mode: pipe entire log through StreamRenderer.
	if dumpRender {
		return runDumpRendered(s)
	}

	data, err := os.ReadFile(s.LogFile)
	if err != nil {
		if os.IsNotExist(err) && tmux.SessionExists(s.TmuxSession) {
			fmt.Println("(waiting for output...)")
			return nil
		}
		return fmt.Errorf("read log: %w", err)
	}

	if dumpJSON {
		// Raw JSON mode: show last N lines of raw NDJSON.
		lines := splitLines(data)
		if len(lines) > dumpLines {
			lines = lines[len(lines)-dumpLines:]
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	}

	// Filter to last N turns if requested.
	if dumpLast {
		dumpTurns = 1
	}
	if dumpTurns > 0 {
		data = filterLastNTurns(data, dumpTurns)
	}

	// Print session header for non-JSON modes.
	printSessionHeader(s)

	if dumpSummary || dumpCondensed {
		text := renderJSONLogSummary(data)
		if dumpCondensed {
			text = renderJSONLogCondensed(data)
		}
		if strings.TrimSpace(text) == "" && len(data) > 0 {
			// Plain text fallback
			lines := splitLines(data)
			if len(lines) > dumpLines {
				lines = lines[len(lines)-dumpLines:]
			}
			for _, l := range lines {
				fmt.Println(l)
			}
			return nil
		}
		lines := splitLines([]byte(text))
		if len(lines) > dumpLines {
			lines = lines[len(lines)-dumpLines:]
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	}

	// Try parsing as NDJSON first. If no JSON events found, treat as plain text
	// (backward compatibility with pre-JSON-mode logs).
	text := renderJSONLog(data)
	if strings.TrimSpace(text) == "" && len(data) > 0 {
		// Likely a plain text log from before JSON mode was enabled.
		lines := splitLines(data)
		if len(lines) > dumpLines {
			lines = lines[len(lines)-dumpLines:]
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	}

	lines := splitLines([]byte(text))
	if len(lines) > dumpLines {
		lines = lines[len(lines)-dumpLines:]
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

// runDumpRendered reads the complete log and renders it through StreamRenderer.
func runDumpRendered(s *session.Session) error {
	data, err := os.ReadFile(s.LogFile)
	if err != nil {
		if os.IsNotExist(err) && tmux.SessionExists(s.TmuxSession) {
			fmt.Println("(waiting for output...)")
			return nil
		}
		return fmt.Errorf("read log: %w", err)
	}

	if dumpLast {
		dumpTurns = 1
	}
	if dumpTurns > 0 {
		data = filterLastNTurns(data, dumpTurns)
	}

	printSessionHeader(s)

	var opts []render.Option
	if dumpNoColor {
		opts = append(opts, render.WithNoColor())
	}
	renderer := render.New(os.Stdout, opts...)
	lines := splitLines(data)
	// Apply --lines limit: render only the last N JSON events.
	if len(lines) > dumpLines {
		lines = lines[len(lines)-dumpLines:]
	}
	for _, line := range lines {
		renderer.RenderLine([]byte(line))
	}
	return nil
}

// runDumpFollow tails the log file and renders events in real-time.
// Stops when the session ends or on Ctrl-C.
func runDumpFollow(s *session.Session) error {
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // inotify unreliable on Docker bind mounts
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		return fmt.Errorf("tail %s: %w", s.LogFile, err)
	}
	defer t.Cleanup()

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		select {
		case <-sig:
			closeDone()
		case <-done:
		}
	}()

	// Also stop when session ends.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !tmux.SessionExists(s.TmuxSession) {
					time.Sleep(500 * time.Millisecond) // let final events flush
					closeDone()
					return
				}
			}
		}
	}()

	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				return nil
			}
			if line.Err != nil {
				continue
			}
			if dumpJSON {
				fmt.Println(line.Text)
			} else {
				text := renderJSONLineForDump(line.Text)
				if text != "" {
					fmt.Print(text)
				}
			}
		case <-done:
			// Drain remaining
			for {
				select {
				case line, ok := <-t.Lines:
					if !ok || line == nil {
						return nil
					}
					if line.Err != nil {
						continue
					}
					if dumpJSON {
						fmt.Println(line.Text)
					} else {
						text := renderJSONLineForDump(line.Text)
						if text != "" {
							fmt.Print(text)
						}
					}
				default:
					return nil
				}
			}
		}
	}
}

// runDumpFollowRendered tails the log and renders with Pi-TUI-like formatting.
func runDumpFollowRendered(s *session.Session) error {
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true,
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		return fmt.Errorf("tail %s: %w", s.LogFile, err)
	}
	defer t.Cleanup()

	var opts []render.Option
	if dumpNoColor {
		opts = append(opts, render.WithNoColor())
	}
	renderer := render.New(os.Stdout, opts...)

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		select {
		case <-sig:
			closeDone()
		case <-done:
		}
	}()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !tmux.SessionExists(s.TmuxSession) {
					time.Sleep(500 * time.Millisecond)
					closeDone()
					return
				}
			}
		}
	}()

	printSessionHeader(s)

	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				return nil
			}
			if line.Err != nil {
				continue
			}
			renderer.RenderLine([]byte(line.Text))
		case <-done:
			for {
				select {
				case line, ok := <-t.Lines:
					if !ok || line == nil {
						return nil
					}
					if line.Err != nil {
						continue
					}
					renderer.RenderLine([]byte(line.Text))
				default:
					return nil
				}
			}
		}
	}
}

// renderJSONLineForDump renders a single NDJSON event for follow mode.
// Similar to renderJSONLog but handles one event at a time.
func renderJSONLineForDump(line string) string {
	if line == "" {
		return ""
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return ""
	}

	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_start":
		msg, _ := event["message"].(map[string]interface{})
		role, _ := msg["role"].(string)
		if role == "user" {
			content, _ := msg["content"].([]interface{})
			for _, c := range content {
				cm, _ := c.(map[string]interface{})
				if t, _ := cm["type"].(string); t == "text" {
					text, _ := cm["text"].(string)
					return fmt.Sprintf("\n> %s\n\n", text)
				}
			}
		}
		if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
			return formatAPIError(msg)
		}
	case "message_update":
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae == nil {
			return ""
		}
		aeType, _ := ae["type"].(string)
		switch aeType {
		case "text_delta":
			delta, _ := ae["delta"].(string)
			return delta
		case "text_start":
			return "\n"
		case "thinking_start":
			return "💭 thinking...\n"
		}
	// Top-level text/thinking events emitted by OpenAI models.
	case "text_delta":
		delta, _ := event["delta"].(string)
		return delta
	case "text_start":
		return "\n"
	case "thinking_start":
		return "💭 thinking...\n"
	case "tool_execution_start":
		toolName, _ := event["toolName"].(string)
		args, _ := event["args"].(map[string]interface{})
		return "\n" + formatToolCall(toolName, args, 120) + "\n"
	case "tool_execution_end":
		isError, _ := event["isError"].(bool)
		result, _ := event["result"].(map[string]interface{})
		if isError {
			errText := formatToolResult(result, 200)
			if errText != "" {
				return "❌ " + errText
			}
			return "❌ error\n"
		}
		return formatToolResult(result, 200)
	case "turn_end":
		msg, _ := event["message"].(map[string]interface{})
		if tokens, costStr, ok := usageSummary(msg); ok {
			return fmt.Sprintf("  [%d tokens%s]\n\n---\n", tokens, costStr)
		}
		return "\n---\n"
	}
	return ""
}

// renderJSONLog parses NDJSON events and produces human-readable text output.
func renderJSONLog(data []byte) string {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	// Increase scanner buffer for large JSON lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "message_start":
			msg, _ := event["message"].(map[string]interface{})
			role, _ := msg["role"].(string)
			if role == "user" {
				content, _ := msg["content"].([]interface{})
				for _, c := range content {
					cm, _ := c.(map[string]interface{})
					if t, _ := cm["type"].(string); t == "text" {
						text, _ := cm["text"].(string)
						out.WriteString(fmt.Sprintf("\n> %s\n\n", text))
					}
				}
			}
			if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
				out.WriteString(formatAPIError(msg))
			}
		case "message_update":
			ae, _ := event["assistantMessageEvent"].(map[string]interface{})
			if ae == nil {
				continue
			}
			aeType, _ := ae["type"].(string)
			switch aeType {
			case "text_delta":
				delta, _ := ae["delta"].(string)
				out.WriteString(delta)
			case "text_start":
				out.WriteString("\n")
			case "thinking_start":
				out.WriteString("💭 thinking...\n")
			case "thinking_end":
				// thinking done, text will follow
			}
		// Top-level event types emitted by OpenAI models (not nested in assistantMessageEvent).
		case "text_delta":
			delta, _ := event["delta"].(string)
			out.WriteString(delta)
		case "text_start":
			out.WriteString("\n")
		case "thinking_start":
			out.WriteString("💭 thinking...\n")
		case "thinking_end":
			// thinking done, text will follow
		case "tool_execution_start":
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			out.WriteString("\n" + formatToolCall(toolName, args, 120) + "\n")
		case "tool_execution_update":
			// Streaming partial results from long-running tools (e.g., bash output).
			// We skip these in dump since tool_execution_end has the final result.
			// Monitor handles these for live streaming.
		case "tool_execution_end":
			isError, _ := event["isError"].(bool)
			result, _ := event["result"].(map[string]interface{})
			if isError {
				errText := formatToolResult(result, 200)
				if errText != "" {
					out.WriteString("❌ " + errText)
				} else {
					out.WriteString("❌ error\n")
				}
			} else {
				out.WriteString(formatToolResult(result, 200))
			}
		case "turn_end":
			msg, _ := event["message"].(map[string]interface{})
			if tokens, costStr, ok := usageSummary(msg); ok {
				out.WriteString(fmt.Sprintf("  [%d tokens%s]\n", tokens, costStr))
			}
			out.WriteString("\n---\n")
		}
	}

	return out.String()
}

// renderJSONLogSummary produces a condensed view: user messages, tool calls,
// final text per turn, and turn separators with token/cost info.
func renderJSONLogSummary(data []byte) string {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var currentText strings.Builder
	turnCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "message_start":
			msg, _ := event["message"].(map[string]interface{})
			role, _ := msg["role"].(string)
			if role == "user" {
				content, _ := msg["content"].([]interface{})
				for _, c := range content {
					cm, _ := c.(map[string]interface{})
					if t, _ := cm["type"].(string); t == "text" {
						text, _ := cm["text"].(string)
						if text != "" {
							out.WriteString(fmt.Sprintf("> %s\n\n", truncateRunesASCII(text, 200)))
						}
					}
				}
			}
			if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
				out.WriteString(formatAPIError(msg))
			}
		case "message_update":
			ae, _ := event["assistantMessageEvent"].(map[string]interface{})
			if ae == nil {
				continue
			}
			aeType, _ := ae["type"].(string)
			switch aeType {
			case "text_delta":
				// Accumulate text, don't print yet
				delta, _ := ae["delta"].(string)
				currentText.WriteString(delta)
			case "text_start":
				currentText.Reset()
			case "text_end":
				// Prefer accumulated delta text, but fall back to the full content on
				// text_end so summaries still work when reading from a tail slice that
				// starts in the middle of a large text_delta event.
				text := strings.TrimSpace(currentText.String())
				if text == "" {
					text, _ = ae["content"].(string)
					text = strings.TrimSpace(text)
				}
				if text != "" {
					out.WriteString(text + "\n")
				}
				currentText.Reset()
			}
		// Top-level event types emitted by OpenAI models.
		case "text_delta":
			delta, _ := event["delta"].(string)
			currentText.WriteString(delta)
		case "text_start":
			currentText.Reset()
		case "text_end":
			text := strings.TrimSpace(currentText.String())
			if text == "" {
				text, _ = event["content"].(string)
				text = strings.TrimSpace(text)
			}
			if text != "" {
				out.WriteString(text + "\n")
			}
			currentText.Reset()
		case "tool_execution_start":
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			out.WriteString(formatToolCall(toolName, args, 120) + "\n")
		case "tool_execution_end":
			isError, _ := event["isError"].(bool)
			result, _ := event["result"].(map[string]interface{})
			if isError {
				errText := formatToolResult(result, 500)
				if errText != "" {
					out.WriteString("❌ " + errText)
				} else {
					out.WriteString("❌ error\n")
				}
			} else {
				out.WriteString(formatToolResult(result, 500))
			}
		case "turn_end":
			// Flush any accumulated text that wasn't closed by text_end
			// (e.g., truncated logs or incomplete streams).
			if text := strings.TrimSpace(currentText.String()); text != "" {
				out.WriteString(text + "\n")
			}
			currentText.Reset()

			turnCount++
			msg, _ := event["message"].(map[string]interface{})
			if tokens, costStr, ok := usageSummary(msg); ok {
				out.WriteString(fmt.Sprintf("  [turn %d · %d tokens%s]\n", turnCount, tokens, costStr))
			}
			out.WriteString("---\n")
		}
	}

	// Flush any remaining accumulated text at end of stream.
	if text := strings.TrimSpace(currentText.String()); text != "" {
		out.WriteString(text + "\n")
	}

	return out.String()
}

type condensedActivity struct {
	text        string
	collapseKey string
	count       int
}

// renderJSONLogCondensed produces a human-friendly activity timeline.
// Activities collapse across turn boundaries so consecutive edits to the same
// file are shown as a single line regardless of how many turns they span.
// Turn cost/token info is aggregated into a footer rather than per-turn lines.
func renderJSONLogCondensed(data []byte) string {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var currentText strings.Builder
	var assistantText strings.Builder
	var activities []condensedActivity
	turnCount := 0
	var totalTokens int
	var totalCost float64

	appendActivity := func(text, collapseKey string) {
		if text == "" {
			return
		}
		if collapseKey != "" && len(activities) > 0 {
			last := &activities[len(activities)-1]
			if last.collapseKey == collapseKey {
				last.count++
				return
			}
		}
		activities = append(activities, condensedActivity{text: text, collapseKey: collapseKey, count: 1})
	}

	appendAssistantChunk := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if assistantText.Len() > 0 {
			assistantText.WriteString("\n")
		}
		assistantText.WriteString(text)
	}

	flushCurrentText := func(fallback string) {
		text := strings.TrimSpace(currentText.String())
		if text == "" {
			text = strings.TrimSpace(fallback)
		}
		currentText.Reset()
		appendAssistantChunk(text)
	}

	flushAssistantText := func() {
		flushCurrentText("")
		if text := strings.TrimSpace(assistantText.String()); text != "" {
			// Emit assistant text preserving original line structure.
			// Wrap each paragraph independently so lists/headings stay intact.
			paragraphs := strings.Split(text, "\n")
			first := true
			for _, para := range paragraphs {
				para = strings.TrimRight(para, " \t")
				if para == "" {
					// Preserve blank lines as empty activity lines.
					appendActivity("", "")
					continue
				}
				wrapped := wordWrap(para, 100)
				for _, wl := range wrapped {
					if first {
						appendActivity("💬 "+wl, "")
						first = false
					} else {
						appendActivity("  "+wl, "")
					}
				}
			}
		}
		assistantText.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "message_start":
			msg, _ := event["message"].(map[string]interface{})
			role, _ := msg["role"].(string)
			if role == "user" {
				// Flush any pending assistant text before a new user prompt.
				flushAssistantText()
				content, _ := msg["content"].([]interface{})
				for _, c := range content {
					cm, _ := c.(map[string]interface{})
					if t, _ := cm["type"].(string); t == "text" {
						text, _ := cm["text"].(string)
						text = singleLineTrimmed(text)
						if text != "" {
							out.WriteString(fmt.Sprintf("> %s\n\n", truncateRunesASCII(text, 200)))
						}
					}
				}
			}
			if stopReason, _ := msg["stopReason"].(string); stopReason == "error" {
				appendActivity(strings.TrimSpace(formatAPIError(msg)), "")
			}
		case "message_update":
			ae, _ := event["assistantMessageEvent"].(map[string]interface{})
			if ae == nil {
				continue
			}
			aeType, _ := ae["type"].(string)
			switch aeType {
			case "text_delta":
				delta, _ := ae["delta"].(string)
				currentText.WriteString(delta)
			case "text_start":
				currentText.Reset()
			case "text_end":
				fallback, _ := ae["content"].(string)
				flushCurrentText(fallback)
			}
		case "text_delta":
			delta, _ := event["delta"].(string)
			currentText.WriteString(delta)
		case "text_start":
			currentText.Reset()
		case "text_end":
			fallback, _ := event["content"].(string)
			flushCurrentText(fallback)
		case "tool_execution_start":
			// Flush any assistant text before tool calls so prose appears
			// before the tools it precedes, not after.
			flushAssistantText()
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			text, collapseKey := formatCondensedToolActivity(toolName, args)
			appendActivity(text, collapseKey)
		case "tool_execution_end":
			isError, _ := event["isError"].(bool)
			result, _ := event["result"].(map[string]interface{})
			if errText := condensedToolErrorText(result, isError); errText != "" {
				appendActivity("⚠ "+truncateRunesASCII(singleLineTrimmed(errText), 150), "")
			}
		case "turn_end":
			turnCount++
			msg, _ := event["message"].(map[string]interface{})
			if tokens, costStr, ok := usageSummary(msg); ok {
				totalTokens += tokens
				_ = costStr
				// Parse cost from the formatted string.
				if costVal, _ := extractCostFromUsage(msg); costVal > 0 {
					totalCost += costVal
				}
			}
		}
	}

	// Flush remaining text/activities.
	flushAssistantText()

	// Emit all activities.
	for _, activity := range activities {
		line := activity.text
		if activity.count > 1 {
			line += fmt.Sprintf(" (×%d)", activity.count)
		}
		out.WriteString("  " + line + "\n")
	}

	// Footer with aggregate stats.
	if turnCount > 0 {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		if totalCost > 0 {
			out.WriteString(fmt.Sprintf("  [%d turns · %d tokens · $%.4f]\n", turnCount, totalTokens, totalCost))
		} else if totalTokens > 0 {
			out.WriteString(fmt.Sprintf("  [%d turns · %d tokens]\n", turnCount, totalTokens))
		} else {
			out.WriteString(fmt.Sprintf("  [%d turns]\n", turnCount))
		}
	}

	return out.String()
}

func formatCondensedToolActivity(toolName string, args map[string]interface{}) (string, string) {
	formatted := strings.TrimPrefix(formatToolCall(toolName, args, 100), "🔧 ")

	switch toolName {
	case "bash":
		if strings.HasPrefix(formatted, "bash: ") {
			return "$ " + strings.TrimPrefix(formatted, "bash: "), ""
		}
		return "$ bash", ""
	case "read", "write", "edit":
		if p, ok := args["path"].(string); ok && p != "" {
			return fmt.Sprintf("%s %s", toolName, p), toolName + ":" + p
		}
	}

	return formatted, ""
}

func condensedToolErrorText(result map[string]interface{}, isError bool) string {
	if !isError {
		return ""
	}
	text := strings.TrimSpace(toolResultText(result))
	if text == "" {
		return "error"
	}
	return text
}

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

// filterLastNTurns returns the NDJSON data for only the last N turns.
// A turn is delimited by turn_start/turn_end events.
func filterLastNTurns(data []byte, n int) []byte {
	if len(data) == 0 {
		return data
	}

	// Find all turn_start byte positions by scanning line by line.
	var turnStartPositions []int
	lineStart := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[lineStart:i]
			if len(line) > 0 && strings.Contains(string(line), `"turn_start"`) {
				var event map[string]interface{}
				if err := json.Unmarshal(line, &event); err == nil {
					if t, _ := event["type"].(string); t == "turn_start" {
						turnStartPositions = append(turnStartPositions, lineStart)
					}
				}
			}
			lineStart = i + 1
		}
	}
	if len(turnStartPositions) <= n {
		return data
	}
	startAt := turnStartPositions[len(turnStartPositions)-n]
	return data[startAt:]
}

// formatAPIError formats an error message from a message_start event with stopReason "error".
func formatAPIError(msg map[string]interface{}) string {
	errMsg, _ := msg["errorMessage"].(string)
	if errMsg == "" {
		return "❌ API error\n"
	}
	// Try to extract a readable message from nested JSON error strings.
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(errMsg), &parsed) == nil {
		if inner, ok := parsed["error"].(map[string]interface{}); ok {
			if m, ok := inner["message"].(string); ok {
				errMsg = m
				// The message itself might be JSON (double-encoded).
				var innerParsed map[string]interface{}
				if json.Unmarshal([]byte(m), &innerParsed) == nil {
					if e2, ok := innerParsed["error"].(map[string]interface{}); ok {
						if m2, ok := e2["message"].(string); ok {
							errMsg = m2
						}
					}
				}
			}
		}
	}
	return fmt.Sprintf("❌ %s\n", truncateRunesASCII(errMsg, 500))
}

// formatToolCall formats a tool_execution_start event into a display string.
// maxCmdLen controls how long bash commands and similar details can be.
func formatToolCall(toolName string, args map[string]interface{}, maxCmdLen int) string {
	msg := "🔧 " + toolName
	switch toolName {
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			msg += ": " + truncateRunesASCII(strings.ReplaceAll(cmd, "\n", " "), maxCmdLen)
		}
	case "read", "write":
		if p, ok := args["path"].(string); ok {
			msg += ": " + p
		}
	case "edit":
		if p, ok := args["path"].(string); ok {
			msg += ": " + p
		}
	case "todo":
		if action, ok := args["action"].(string); ok {
			msg += " " + action
			if title, ok := args["title"].(string); ok {
				msg += ": " + title
			} else if id, ok := args["id"].(string); ok {
				msg += " " + id
			}
		}
	case "send_to_session":
		if name, ok := args["sessionName"].(string); ok {
			msg += " → " + name
		} else if id, ok := args["sessionId"].(string); ok {
			msg += " → " + id
		}
	case "AskUserQuestion":
		if q, ok := args["question"].(string); ok {
			msg += ": " + truncateRunesASCII(q, maxCmdLen)
		}
	}
	return msg
}

// formatToolResult formats the result text from a tool_execution_end event.
// maxLen controls the truncation limit for the result text (in runes).
func formatToolResult(result map[string]interface{}, maxLen int) string {
	text := toolResultText(result)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("→ %s\n", truncateRunesASCII(text, maxLen))
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
