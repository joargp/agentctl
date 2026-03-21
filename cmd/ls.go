package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List agent sessions",
	RunE:  runLs,
}

var (
	lsModel   string
	lsSince   string
	lsTask    string
	lsCwd     string
	lsRunning bool
	lsDone    bool
	lsQuiet   bool
)

func init() {
	lsCmd.Flags().StringVar(&lsModel, "model", "", "filter by model name (substring match)")
	lsCmd.Flags().StringVar(&lsSince, "since", "", "show only sessions started within this duration (e.g. 1h, 30m, 2d)")
	lsCmd.Flags().StringVar(&lsTask, "task", "", "filter by task content (substring match)")
	lsCmd.Flags().StringVar(&lsCwd, "cwd", "", "filter by working directory (substring match)")
	lsCmd.Flags().BoolVar(&lsRunning, "running", false, "show only running sessions")
	lsCmd.Flags().BoolVar(&lsDone, "done", false, "show only completed sessions")
	lsCmd.Flags().BoolVarP(&lsQuiet, "quiet", "q", false, "print only session IDs (for scripting)")
	rootCmd.AddCommand(lsCmd)
}

func parseDuration(s string) (time.Duration, error) {
	// Support "d" suffix for days in addition to Go's standard durations.
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err == nil {
			return time.Duration(days * float64(24*time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

func runLs(_ *cobra.Command, _ []string) error {
	sessions, err := session.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	// Sort by most recent first.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})

	var sinceFilter time.Duration
	if lsSince != "" {
		var err error
		sinceFilter, err = parseDuration(lsSince)
		if err != nil {
			return fmt.Errorf("invalid --since duration: %w", err)
		}
	}

	// Quick-exit for quiet/scripting mode.
	if lsQuiet {
		return runLsQuiet(sessions, sinceFilter)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Check if any session has a name set.
	hasNames := false
	for _, s := range sessions {
		if s.Name != "" {
			hasNames = true
			break
		}
	}

	if hasNames {
		fmt.Fprintln(w, "ID\tNAME\tMODEL\tSTATUS\tAGE\tTURNS\tCOST\tTASK")
	} else {
		fmt.Fprintln(w, "ID\tMODEL\tSTATUS\tAGE\tTURNS\tCOST\tTASK")
	}

	for _, s := range sessions {
		if lsModel != "" && !strings.Contains(s.Model, lsModel) {
			continue
		}
		if lsTask != "" && !strings.Contains(strings.ToLower(s.Task), strings.ToLower(lsTask)) {
			continue
		}
		if lsCwd != "" && !strings.Contains(s.Cwd, lsCwd) {
			continue
		}
		if sinceFilter > 0 && time.Since(s.StartedAt) > sinceFilter {
			continue
		}
		running := tmux.SessionExists(s.TmuxSession)
		if lsRunning && !running {
			continue
		}
		if lsDone && running {
			continue
		}
		status := "done"
		if running {
			// Show current activity for running sessions.
			// Only read the tail of the log for performance on large files.
			data := readTail(s.LogFile, 64*1024)
			if len(data) > 0 {
				state, _ := session.ParseLastActivity(data)
				status = state
			} else {
				status = "starting"
			}
		}
		age := time.Since(s.StartedAt).Round(time.Second)
		task := strings.ReplaceAll(s.Task, "\n", " ")
		task = strings.TrimSpace(task)
		taskRunes := []rune(task)
		if len(taskRunes) > 50 {
			task = string(taskRunes[:47]) + "..."
		}
		cost := extractTotalCost(s.LogFile)
		costStr := ""
		if cost > 0 {
			costStr = fmt.Sprintf("$%.4f", cost)
		}
		turns := countTurns(s.LogFile)
		turnsStr := ""
		if turns > 0 {
			turnsStr = strconv.Itoa(turns)
		}
		if hasNames {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Model, status, age, turnsStr, costStr, task)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Model, status, age, turnsStr, costStr, task)
		}
	}
	return w.Flush()
}

func runLsQuiet(sessions []*session.Session, sinceFilter time.Duration) error {
	for _, s := range sessions {
		if lsModel != "" && !strings.Contains(s.Model, lsModel) {
			continue
		}
		if lsTask != "" && !strings.Contains(strings.ToLower(s.Task), strings.ToLower(lsTask)) {
			continue
		}
		if lsCwd != "" && !strings.Contains(s.Cwd, lsCwd) {
			continue
		}
		if sinceFilter > 0 && time.Since(s.StartedAt) > sinceFilter {
			continue
		}
		running := tmux.SessionExists(s.TmuxSession)
		if lsRunning && !running {
			continue
		}
		if lsDone && running {
			continue
		}
		fmt.Println(s.ID)
	}
	return nil
}

// readTail reads the last n bytes of a file. Returns nil on error.
func readTail(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}

	size := info.Size()
	if size <= n {
		data, _ := os.ReadFile(path)
		return data
	}

	// Seek to tail and read
	buf := make([]byte, n)
	_, err = f.ReadAt(buf, size-n)
	if err != nil {
		return nil
	}
	return buf
}

// countTurns counts the number of turn_end events in a log file.
// Uses line-by-line scanning to avoid loading huge logs into memory.
func countTurns(logFile string) int {
	f, err := os.Open(logFile)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"turn_end"`) {
			count++
		}
	}
	return count
}

// extractTotalCost scans the JSON log for turn_end events and sums up costs.
// Uses line-by-line scanning to avoid loading huge logs into memory.
func extractTotalCost(logFile string) float64 {
	f, err := os.Open(logFile)
	if err != nil {
		return 0
	}
	defer f.Close()

	var totalCost float64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Quick filter: only parse lines that might have cost data
		if !strings.Contains(line, `"turn_end"`) {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if eventType, _ := event["type"].(string); eventType == "turn_end" {
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
				totalCost += cost
			}
		}
	}

	return totalCost
}
