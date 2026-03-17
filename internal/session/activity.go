package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// Activity describes the current agent activity as derived from one NDJSON event.
// Status is the human-readable progress string emitted for external notifications.
// It is empty for events that should not produce a progress update.
type Activity struct {
	State  string
	Detail string
	Status string
}

// ParseLastActivity scans the JSON log and returns the last meaningful activity
// state and detail for status-style summaries.
func ParseLastActivity(data []byte) (string, string) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var lastState string
	var lastDetail string
	turnCount := 0

	for scanner.Scan() {
		activity := ParseActivityLine(scanner.Text(), &turnCount)
		if activity.State == "" {
			continue
		}
		lastState = activity.State
		lastDetail = activity.Detail
	}

	if lastState == "" {
		return "starting", ""
	}
	return lastState, lastDetail
}

// ParseActivityLine parses a single NDJSON line into an activity update.
func ParseActivityLine(line string, turnCount *int) Activity {
	if strings.TrimSpace(line) == "" {
		return Activity{}
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return Activity{}
	}
	return ParseActivityEvent(event, turnCount)
}

// ParseActivityEvent converts a single pi JSON event into both status-command
// activity fields and progress text for external notifications.
func ParseActivityEvent(event map[string]interface{}, turnCount *int) Activity {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_update":
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae == nil {
			return Activity{}
		}
		aeType, _ := ae["type"].(string)
		switch aeType {
		case "thinking_start":
			return Activity{State: "thinking", Status: "💭 Thinking..."}
		case "thinking_end":
			return Activity{State: "writing"}
		case "text_delta":
			delta, _ := ae["delta"].(string)
			return Activity{State: "writing", Detail: truncateActivityText(delta, 60)}
		case "text_start", "text_end":
			return Activity{State: "writing"}
		}
	case "tool_execution_start":
		toolName, _ := event["toolName"].(string)
		args, _ := event["args"].(map[string]interface{})
		activity := Activity{State: "running " + toolName, Status: "🔧 " + toolName}
		switch toolName {
		case "bash":
			if cmd, ok := args["command"].(string); ok {
				activity.Detail = truncateActivityText(cmd, 60)
				activity.Status = fmt.Sprintf("🔧 Running: `%s`", truncateActivityText(cmd, 80))
			}
		case "edit":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("✏️ Editing `%s`", path)
			}
		case "write":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("📝 Writing `%s`", path)
			}
		case "read":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("📖 Reading `%s`", path)
			}
		}
		return activity
	case "tool_execution_end":
		isError, _ := event["isError"].(bool)
		if isError {
			return Activity{State: "writing", Status: "❌ Tool error"}
		}
		return Activity{State: "writing"}
	case "turn_start":
		if turnCount != nil {
			*turnCount = *turnCount + 1
		}
		return Activity{}
	case "turn_end":
		turn := safeCompletedTurnNumber(turnCount)
		status := formatTurnCompleteStatus(turn, event)
		return Activity{State: fmt.Sprintf("completed turn %d", turn), Status: status}
	case "agent_end":
		return Activity{State: fmt.Sprintf("finished (%d turns)", actualTurnCount(turnCount))}
	}

	return Activity{}
}

// FormatEventStatus returns the progress status string for an event.
func FormatEventStatus(event map[string]interface{}, turnCount *int) string {
	return ParseActivityEvent(event, turnCount).Status
}

func safeCompletedTurnNumber(turnCount *int) int {
	if turnCount == nil || *turnCount <= 0 {
		return 1
	}
	return *turnCount
}

func actualTurnCount(turnCount *int) int {
	if turnCount == nil || *turnCount < 0 {
		return 0
	}
	return *turnCount
}

func formatTurnCompleteStatus(turn int, event map[string]interface{}) string {
	msg, _ := event["message"].(map[string]interface{})
	usage, _ := msg["usage"].(map[string]interface{})
	if usage == nil {
		return fmt.Sprintf("✅ Turn %d complete", turn)
	}

	tokens, hasTokens := usage["totalTokens"].(float64)
	costInfo, _ := usage["cost"].(map[string]interface{})
	total, hasCost := costInfo["total"].(float64)

	switch {
	case hasTokens && hasCost && total > 0:
		return fmt.Sprintf("✅ Turn %d complete (%d tokens, $%.4f)", turn, int(tokens), total)
	case hasTokens:
		return fmt.Sprintf("✅ Turn %d complete (%d tokens)", turn, int(tokens))
	default:
		return fmt.Sprintf("✅ Turn %d complete", turn)
	}
}

func truncateActivityText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
