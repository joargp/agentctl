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
  agentctl dump abc123 --lines 200
  agentctl dump abc123 --json
  agentctl dump abc123 --follow`,
	Args: cobra.ExactArgs(1),
	RunE: runDump,
}

var (
	dumpLines   int
	dumpJSON    bool
	dumpFollow  bool
	dumpSummary bool
)

func init() {
	dumpCmd.Flags().IntVarP(&dumpLines, "lines", "n", 100, "number of lines to show")
	dumpCmd.Flags().BoolVar(&dumpJSON, "json", false, "output raw JSON events")
	dumpCmd.Flags().BoolVarP(&dumpFollow, "follow", "f", false, "follow output like tail -f (rendered or raw JSON)")
	dumpCmd.Flags().BoolVar(&dumpSummary, "summary", false, "condensed output (tool calls + final text only, no intermediate deltas)")
	rootCmd.AddCommand(dumpCmd)
}

func runDump(_ *cobra.Command, args []string) error {
	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	if dumpFollow {
		return runDumpFollow(s)
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

	if dumpSummary {
		text := renderJSONLogSummary(data)
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
		<-sig
		closeDone()
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
	case "tool_execution_start":
		toolName, _ := event["toolName"].(string)
		args, _ := event["args"].(map[string]interface{})
		msg := fmt.Sprintf("\n🔧 %s", toolName)
		if toolName == "bash" {
			if cmd, ok := args["command"].(string); ok {
				if len(cmd) > 120 {
					cmd = cmd[:117] + "..."
				}
				msg += ": " + cmd
			}
		} else if toolName == "read" || toolName == "write" || toolName == "edit" {
			if p, ok := args["path"].(string); ok {
				msg += ": " + p
			}
		}
		return msg + "\n"
	case "tool_execution_end":
		isError, _ := event["isError"].(bool)
		if isError {
			return "❌ error\n"
		}
		result, _ := event["result"].(map[string]interface{})
		if result != nil {
			content, _ := result["content"].([]interface{})
			for _, c := range content {
				cm, _ := c.(map[string]interface{})
				if t, _ := cm["type"].(string); t == "text" {
					text, _ := cm["text"].(string)
					if text != "" {
						if len(text) > 200 {
							text = text[:197] + "..."
						}
						return fmt.Sprintf("→ %s\n", text)
					}
				}
			}
		}
	case "turn_end":
		// Extract token/cost info
		msg, _ := event["message"].(map[string]interface{})
		if msg != nil {
			usage, _ := msg["usage"].(map[string]interface{})
			if usage != nil {
				tokens, _ := usage["totalTokens"].(float64)
				costInfo, _ := usage["cost"].(map[string]interface{})
				if tokens > 0 {
					costStr := ""
					if costInfo != nil {
						if total, ok := costInfo["total"].(float64); ok && total > 0 {
							costStr = fmt.Sprintf(" $%.4f", total)
						}
					}
					return fmt.Sprintf("  [%d tokens%s]\n\n---\n", int(tokens), costStr)
				}
			}
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
			out.WriteString(fmt.Sprintf("\n🔧 %s", toolName))
			// Show brief args for common tools
			if toolName == "bash" {
				if cmd, ok := args["command"].(string); ok {
					if len(cmd) > 120 {
						cmd = cmd[:117] + "..."
					}
					out.WriteString(fmt.Sprintf(": %s", cmd))
				}
			} else if toolName == "read" || toolName == "write" || toolName == "edit" {
				if p, ok := args["path"].(string); ok {
					out.WriteString(fmt.Sprintf(": %s", p))
				}
			}
			out.WriteString("\n")
		case "tool_execution_update":
			// Streaming partial results from long-running tools (e.g., bash output).
			// We skip these in dump since tool_execution_end has the final result.
			// Monitor handles these for live streaming.
		case "tool_execution_end":
			toolName, _ := event["toolName"].(string)
			result, _ := event["result"].(map[string]interface{})
			isError, _ := event["isError"].(bool)
			if isError {
				out.WriteString("❌ error")
			}
			// Show brief result for tool outputs
			if result != nil {
				content, _ := result["content"].([]interface{})
				for _, c := range content {
					cm, _ := c.(map[string]interface{})
					if t, _ := cm["type"].(string); t == "text" {
						text, _ := cm["text"].(string)
						if text != "" {
							if len(text) > 200 {
								text = text[:197] + "..."
							}
							_ = toolName // suppress unused warning
							out.WriteString(fmt.Sprintf("→ %s\n", text))
						}
					}
				}
			}
		case "turn_end":
			// Extract token/cost info from turn_end message
			msg, _ := event["message"].(map[string]interface{})
			if msg != nil {
				usage, _ := msg["usage"].(map[string]interface{})
				if usage != nil {
					tokens, _ := usage["totalTokens"].(float64)
					costInfo, _ := usage["cost"].(map[string]interface{})
					if tokens > 0 {
						costStr := ""
						if costInfo != nil {
							if total, ok := costInfo["total"].(float64); ok && total > 0 {
								costStr = fmt.Sprintf(" $%.4f", total)
							}
						}
						out.WriteString(fmt.Sprintf("  [%d tokens%s]\n", int(tokens), costStr))
					}
				}
			}
			out.WriteString("\n---\n")
		}
	}

	return out.String()
}

// renderJSONLogSummary produces a condensed view: tool calls + final text per turn.
// Skips intermediate text_delta events and thinking.
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
				// Print the accumulated text block
				text := strings.TrimSpace(currentText.String())
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
			if text != "" {
				out.WriteString(text + "\n")
			}
			currentText.Reset()
		case "tool_execution_start":
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			out.WriteString(fmt.Sprintf("🔧 %s", toolName))
			if toolName == "bash" {
				if cmd, ok := args["command"].(string); ok {
					if len(cmd) > 120 {
						cmd = cmd[:117] + "..."
					}
					out.WriteString(": " + cmd)
				}
			} else if toolName == "read" || toolName == "write" || toolName == "edit" {
				if p, ok := args["path"].(string); ok {
					out.WriteString(": " + p)
				}
			}
			out.WriteString("\n")
		case "tool_execution_end":
			isError, _ := event["isError"].(bool)
			if isError {
				out.WriteString("❌ error\n")
			} else {
				result, _ := event["result"].(map[string]interface{})
				if result != nil {
					content, _ := result["content"].([]interface{})
					for _, c := range content {
						cm, _ := c.(map[string]interface{})
						if t, _ := cm["type"].(string); t == "text" {
							text, _ := cm["text"].(string)
							if text != "" {
								if len(text) > 200 {
									text = text[:197] + "..."
								}
								out.WriteString("→ " + text + "\n")
							}
						}
					}
				}
			}
		case "turn_end":
			turnCount++
			out.WriteString("---\n")
		}
	}

	return out.String()
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
