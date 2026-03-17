package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:               "status [id]",
	Short:             "Show a one-line summary of what an agent is currently doing",
	ValidArgsFunction: completeSessionIDs,
	Long: `Parses the JSON log to determine the agent's current activity.
Shows what tool is running, if the agent is thinking, or writing text.
Without an ID, shows status for all running sessions.

Examples:
  agentctl status abc123
  agentctl status`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runStatusAll()
	}

	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	printSessionStatus(s)
	return nil
}

func runStatusAll() error {
	sessions, err := session.List()
	if err != nil {
		return err
	}

	found := false
	for _, s := range sessions {
		if tmux.SessionExists(s.TmuxSession) {
			printSessionStatus(s)
			found = true
		}
	}
	if !found {
		fmt.Println("no running sessions")
	}
	return nil
}

func printSessionStatus(s *session.Session) {
	running := tmux.SessionExists(s.TmuxSession)
	age := time.Since(s.StartedAt).Round(time.Second)

	state := "unknown"
	detail := ""

	// Only read tail of log for performance on large files.
	data := readTail(s.LogFile, 64*1024)
	if len(data) > 0 {
		state, detail = parseLastActivity(data)
	} else if running {
		state = "starting"
	}

	statusLabel := "done"
	if running {
		statusLabel = "running"
	}

	label := s.Label()
	if detail != "" {
		fmt.Printf("%s  %s  %s  %s  %s: %s\n", s.ID, label, statusLabel, age, state, detail)
	} else {
		fmt.Printf("%s  %s  %s  %s  %s\n", s.ID, label, statusLabel, age, state)
	}
}

// parseLastActivity scans the JSON log from the end to determine what the agent
// is currently doing. Returns a state label and optional detail string.
func parseLastActivity(data []byte) (string, string) {
	// Scan backward through the log for the last meaningful event.
	// We read the whole thing but only care about the last few events.
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var lastState string
	var lastDetail string
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
			case "thinking_start":
				lastState = "thinking"
				lastDetail = ""
			case "thinking_end":
				lastState = "writing"
				lastDetail = ""
			case "text_delta":
				lastState = "writing"
				delta, _ := ae["delta"].(string)
				lastDetail = truncate(delta, 60)
			case "text_end":
				lastState = "writing"
			}
		case "tool_execution_start":
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			lastState = "running " + toolName
			if toolName == "bash" {
				if cmd, ok := args["command"].(string); ok {
					lastDetail = truncate(cmd, 60)
				}
			} else if toolName == "read" || toolName == "write" || toolName == "edit" {
				if p, ok := args["path"].(string); ok {
					lastDetail = p
				}
			}
		case "tool_execution_end":
			lastState = "writing"
			lastDetail = ""
		case "turn_start":
			turnCount++
		case "turn_end":
			lastState = fmt.Sprintf("completed turn %d", turnCount)
			lastDetail = ""
		case "agent_end":
			lastState = fmt.Sprintf("finished (%d turns)", turnCount)
			lastDetail = ""
		}
	}

	if lastState == "" {
		return "starting", ""
	}
	return lastState, lastDetail
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
