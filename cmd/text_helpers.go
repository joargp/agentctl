package cmd

import (
	"fmt"
	"strings"
)

func truncateRunesASCII(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// wordWrap splits text into lines of at most maxWidth characters,
// breaking at word boundaries when possible.
func wordWrap(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) <= maxWidth {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	lines = append(lines, current)
	return lines
}

func singleLineTrimmed(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
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
		cost = fmt.Sprintf(" $%.4f", total)
	}
	return int(tokens), cost, true
}

func extractCostFromUsage(msg map[string]interface{}) (float64, error) {
	if msg == nil {
		return 0, fmt.Errorf("message is nil")
	}
	usage, _ := msg["usage"].(map[string]interface{})
	if usage == nil {
		return 0, fmt.Errorf("usage missing")
	}
	costInfo, _ := usage["cost"].(map[string]interface{})
	if costInfo == nil {
		return 0, fmt.Errorf("cost missing")
	}
	total, ok := costInfo["total"].(float64)
	if !ok {
		return 0, fmt.Errorf("cost.total missing or not numeric")
	}
	return total, nil
}
