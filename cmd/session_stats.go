package cmd

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/joargp/agentctl/internal/session"
)

type logStats struct {
	Turns     int
	TotalCost float64
}

func scanLogStats(logFile string) logStats {
	f, err := os.Open(logFile)
	if err != nil {
		return logStats{}
	}
	defer f.Close()

	stats := logStats{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"turn_end"`) {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if eventType, _ := event["type"].(string); eventType != "turn_end" {
			continue
		}
		stats.Turns++
		msg, _ := event["message"].(map[string]interface{})
		if msg == nil {
			continue
		}
		usage, _ := msg["usage"].(map[string]interface{})
		if usage == nil {
			continue
		}
		costInfo, _ := usage["cost"].(map[string]interface{})
		if costInfo == nil {
			continue
		}
		if cost, ok := costInfo["total"].(float64); ok {
			stats.TotalCost += cost
		}
	}

	return stats
}

func getSessionLogStats(s *session.Session, running bool) logStats {
	// Running sessions always scan live.
	if running {
		return scanLogStats(s.LogFile)
	}
	// Use cached values if available. Turns > 0 is the cache indicator since
	// every completed session has at least one turn. TotalCost can legitimately
	// be zero (e.g. no cost tracking), so we don't use it as a cache signal.
	if s.Turns > 0 {
		return logStats{Turns: s.Turns, TotalCost: s.TotalCost}
	}
	return scanLogStats(s.LogFile)
}

func cacheSessionLogStats(s *session.Session) error {
	stats := scanLogStats(s.LogFile)
	s.Turns = stats.Turns
	s.TotalCost = stats.TotalCost
	return session.Save(s)
}

// countTurns counts the number of turn_end events in a log file.
func countTurns(logFile string) int {
	return scanLogStats(logFile).Turns
}

// extractTotalCost scans the JSON log for turn_end events and sums up costs.
func extractTotalCost(logFile string) float64 {
	return scanLogStats(logFile).TotalCost
}
