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
			return Activity{State: "thinking", Status: "thinking"}
		case "thinking_end":
			// If thinking_end includes content, extract a summary
			if content, ok := ae["content"].(string); ok && content != "" {
				summary := formatThinkingSummary(content)
				if summary != "" {
					return Activity{State: "writing", Status: summary}
				}
			}
			return Activity{State: "writing"}
		case "text_delta":
			delta, _ := ae["delta"].(string)
			return Activity{State: "writing", Detail: truncateActivityText(delta, 60)}
		case "text_start":
			return Activity{State: "writing"}
		case "text_end":
			content, _ := ae["content"].(string)
			return Activity{State: "writing", Status: formatAssistantTextStatus(content)}
		}
	case "tool_execution_start":
		toolName, _ := event["toolName"].(string)
		args, _ := event["args"].(map[string]interface{})
		activity := Activity{State: "running " + toolName, Status: toolName}
		switch toolName {
		case "bash":
			if cmd, ok := args["command"].(string); ok {
				activity.Detail = truncateActivityText(cmd, 60)
				activity.Status = fmt.Sprintf("running `%s`", truncateActivityText(cmd, 80))
			}
		case "edit":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("editing `%s`", path)
			}
		case "write":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("writing `%s`", path)
			}
		case "read":
			if path, ok := args["path"].(string); ok {
				activity.Detail = path
				activity.Status = fmt.Sprintf("reading `%s`", path)
			}
		}
		return activity
	case "tool_execution_end":
		isError, _ := event["isError"].(bool)
		if isError {
			return Activity{State: "writing", Status: "tool error"}
		}
		return Activity{State: "writing"}
	case "turn_start":
		if turnCount != nil {
			*turnCount = *turnCount + 1
		}
		return Activity{}
	case "turn_end":
		turn := safeCompletedTurnNumber(turnCount)
		return Activity{State: fmt.Sprintf("completed turn %d", turn)}
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

func formatAssistantTextStatus(content string) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return ""
	}

	for _, prefix := range []string{"Sure! ", "Sure, ", "Okay, ", "Okay! ", "Alright, ", "All right, "} {
		content = strings.TrimPrefix(content, prefix)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	// Truncate long text to a useful snippet instead of dropping it entirely
	if len(content) > 200 {
		// Try to break at a sentence boundary
		truncated := content[:200]
		if idx := strings.LastIndexAny(truncated, ".!?"); idx > 100 {
			return truncated[:idx+1]
		}
		// Otherwise break at a word boundary
		if idx := strings.LastIndex(truncated, " "); idx > 100 {
			return truncated[:idx] + "…"
		}
		return truncated + "…"
	}
	return content
}

func formatThinkingSummary(content string) string {
	// Collapse whitespace and take first meaningful line as summary
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	// Take the first non-empty line
	lines := strings.SplitN(content, "\n", 10)
	var firstLine string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			firstLine = line
			break
		}
	}
	if firstLine == "" {
		return ""
	}
	if len(firstLine) > 150 {
		if idx := strings.LastIndex(firstLine[:150], " "); idx > 80 {
			return "_" + firstLine[:idx] + "…_"
		}
		return "_" + firstLine[:147] + "…_"
	}
	return "_" + firstLine + "_"
}

func truncateActivityText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
