package cmd

import (
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

var monitorCmd = &cobra.Command{
	Use:               "monitor [id...]",
	Short:             "Stream live output from one or more agent sessions",
	ValidArgsFunction: completeSessionIDs,
	Long: `Tail the JSON log files of one or more sessions with labeled, interleaved output.
Parses NDJSON events and displays human-readable text (assistant messages, tool calls).
If no IDs are given, monitors all sessions that are currently running.

Examples:
  agentctl monitor abc123
  agentctl monitor abc123 def456`,
	RunE: runMonitor,
}

func init() {
	rootCmd.AddCommand(monitorCmd)
}

func runMonitor(_ *cobra.Command, args []string) error {
	ids := args

	if len(ids) == 0 {
		all, err := session.List()
		if err != nil {
			return err
		}
		// Default: only currently running sessions.
		for _, s := range all {
			if tmux.SessionExists(s.TmuxSession) {
				ids = append(ids, s.ID)
			}
		}
	}
	if len(ids) == 0 {
		fmt.Println("no running sessions to monitor")
		return nil
	}

	type line struct {
		label string
		text  string
	}

	out := make(chan line, 64)
	done := make(chan struct{})

	// Pre-load sessions and detect duplicate labels so we can append the short ID only when needed.
	type entry struct {
		s     *session.Session
		label string
	}
	var entries []entry
	labelCount := map[string]int{}
	for _, id := range ids {
		s, err := session.Load(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: session %s not found: %v\n", id, err)
			continue
		}
		if !tmux.SessionExists(s.TmuxSession) {
			fmt.Fprintf(os.Stderr, "warn: session %s is not running\n", id)
			continue
		}
		labelCount[s.Label()]++
		entries = append(entries, entry{s: s})
	}
	for i := range entries {
		lbl := entries[i].s.Label()
		if labelCount[lbl] > 1 {
			lbl = fmt.Sprintf("%s/%s", lbl, entries[i].s.ID[:4])
		}
		entries[i].label = fmt.Sprintf("[%s]", lbl)
	}

	for _, e := range entries {
		s := e.s
		label := e.label

		go func(s *session.Session, label string) {
			t, err := tail.TailFile(s.LogFile, tail.Config{
				Follow:    true,
				ReOpen:    true,
				MustExist: false,
				Poll:      true, // inotify unreliable on Docker bind mounts
				Logger:    tail.DiscardingLogger,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "tail %s: %v\n", s.ID, err)
				return
			}
			defer t.Cleanup()
			var buf strings.Builder
			inThinking := false
			flushTimer := time.NewTimer(0)
			if !flushTimer.Stop() {
				<-flushTimer.C
			}

			flush := func() {
				if buf.Len() == 0 {
					return
				}
				s := buf.String()
				buf.Reset()
				for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
					if ln == "" {
						continue
					}
					if inThinking {
						ln = "💭 " + ln
					}
					out <- line{label: label, text: ln}
				}
			}

			stopFlushTimer := func() {
				if !flushTimer.Stop() {
					select {
					case <-flushTimer.C:
					default:
					}
				}
			}

			for {
				select {
				case l, ok := <-t.Lines:
					if !ok {
						flush()
						return
					}
					if l.Err != nil {
						continue
					}
					delta, deltaKind, other := classifyEvent(l.Text)
					if delta != "" {
						if deltaKind == "thinking" && !inThinking {
							flush()
							inThinking = true
						} else if deltaKind == "text" && inThinking {
							flush()
							inThinking = false
						}
						buf.WriteString(delta)
						if strings.Contains(delta, "\n") {
							parts := strings.Split(buf.String(), "\n")
							for _, p := range parts[:len(parts)-1] {
								if p != "" {
									if inThinking {
										p = "💭 " + p
									}
									out <- line{label: label, text: p}
								}
							}
							buf.Reset()
							buf.WriteString(parts[len(parts)-1])
						}
						if buf.Len() > 0 {
							flushTimer.Reset(150 * time.Millisecond)
						}
					} else {
						stopFlushTimer()
						flush()
						if other != "" {
							out <- line{label: label, text: other}
						}
					}
				case <-flushTimer.C:
					flush()
				case <-done:
					stopFlushTimer()
					flush()
					return
				}
			}
		}(s, label)
	}

	// Fail fast if every requested ID is invalid or not currently running.
	if len(entries) == 0 {
		return fmt.Errorf("no running sessions to monitor")
	}

	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	// Stop on Ctrl-C
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

	// Auto-stop when all monitored sessions finish
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				allDone := true
				for _, e := range entries {
					if tmux.SessionExists(e.s.TmuxSession) {
						allDone = false
						break
					}
				}
				if allDone {
					time.Sleep(1 * time.Second) // let final events flush
					closeDone()
					return
				}
			}
		}
	}()

	// Determine label width for alignment.
	maxLabel := 0
	for _, e := range entries {
		if l := len(e.label); l > maxLabel {
			maxLabel = l
		}
	}
	format := fmt.Sprintf("%%-%ds  %%s\n", maxLabel)

	for {
		select {
		case l := <-out:
			fmt.Fprintf(os.Stdout, format, l.label, l.text)
		case <-done:
			// Drain remaining
			for {
				select {
				case l := <-out:
					fmt.Fprintf(os.Stdout, format, l.label, l.text)
				default:
					return nil
				}
			}
		}
	}
}

// renderJSONLine parses a single NDJSON event and returns human-readable text, or "" to skip.
func renderJSONLine(line string) string {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return ""
	}

	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_update":
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae == nil {
			return ""
		}
		aeType, _ := ae["type"].(string)
		switch aeType {
		case "thinking_start":
			return "💭 thinking..."
		}
	// Top-level thinking_start emitted by OpenAI models.
	case "thinking_start":
		return "💭 thinking..."
	case "tool_execution_start":
		toolName, _ := event["toolName"].(string)
		args, _ := event["args"].(map[string]interface{})
		return formatToolCall(toolName, args, 80)
	case "tool_execution_update":
		// Streaming partial output from tool (e.g., bash stdout)
		partialResult, _ := event["partialResult"].(map[string]interface{})
		if result := formatToolResult(partialResult, 200); result != "" {
			// Replace "→ " prefix with "  " for partial results, strip trailing newline
			result = strings.TrimSuffix(strings.TrimPrefix(result, "→ "), "\n")
			return "  " + result
		}
		return ""
	case "tool_execution_end":
		isError, _ := event["isError"].(bool)
		if isError {
			return "❌ tool error"
		}
		return ""
	case "turn_end":
		msg, _ := event["message"].(map[string]interface{})
		if tokens, costStr, ok := usageSummary(msg); ok {
			return fmt.Sprintf("[%d tokens%s] ---", tokens, costStr)
		}
		return "---"
	}
	return ""
}

// classifyEvent parses a JSON line. If it is a text or thinking delta, returns the delta text and kind.
// Otherwise returns the rendered string from renderJSONLine.
func classifyEvent(line string) (delta string, deltaKind string, other string) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", "", ""
	}
	eventType, _ := event["type"].(string)
	if eventType == "message_update" {
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae != nil {
			aeType, _ := ae["type"].(string)
			if aeType == "thinking_delta" {
				d, _ := ae["delta"].(string)
				return d, "thinking", ""
			}
			if aeType == "text_delta" {
				d, _ := ae["delta"].(string)
				return d, "text", ""
			}
		}
	}
	// Top-level delta events emitted by OpenAI models.
	if eventType == "thinking_delta" {
		d, _ := event["delta"].(string)
		return d, "thinking", ""
	}
	if eventType == "text_delta" {
		d, _ := event["delta"].(string)
		return d, "text", ""
	}
	return "", "", renderJSONLine(line)
}
