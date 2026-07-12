package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Reclaim disk by removing old finished session logs",
	Long: `Delete bulky log artifacts for finished sessions older than a cutoff,
while keeping session metadata (task, model, turns, cost) so history remains
queryable via ls/costs/status.

Running sessions are never touched. Use --dry-run to preview.

Examples:
  agentctl prune --older-than 30d --dry-run
  agentctl prune --older-than 30d
  agentctl prune --older-than 14d --cwd /repos/myapp`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

var (
	pruneOlderThan string
	pruneDryRun    bool
	pruneCwd       string
	pruneModel     string
)

var listTmuxSessionsForPrune = tmux.ListSessions

func init() {
	pruneCmd.Flags().StringVar(&pruneOlderThan, "older-than", "", "only prune sessions started longer ago than this duration (e.g. 1h, 30m, 2d); required")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "report candidates and reclaimable bytes; delete nothing")
	pruneCmd.Flags().StringVar(&pruneCwd, "cwd", "", "filter by working directory (substring match)")
	pruneCmd.Flags().StringVar(&pruneModel, "model", "", "filter by model name (substring match)")
	rootCmd.AddCommand(pruneCmd)
}

type pruneCandidate struct {
	s     *session.Session
	files []string
	size  int64
}

func runPrune(_ *cobra.Command, _ []string) error {
	if pruneOlderThan == "" {
		return fmt.Errorf("--older-than is required (e.g. --older-than 30d)")
	}
	olderThan, err := parseDuration(pruneOlderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than duration: %w", err)
	}

	sessions, err := session.List()
	if err != nil {
		return err
	}

	runningSessions := listTmuxSessionsForPrune()

	var (
		candidates     []pruneCandidate
		runningSkipped int
		tooNewSkipped  int
		alreadyClean   int
	)
	for _, s := range sessions {
		if runningSessions[s.TmuxSession] {
			runningSkipped++
			continue
		}
		if time.Since(s.StartedAt) < olderThan {
			tooNewSkipped++
			continue
		}
		if pruneCwd != "" && !strings.Contains(s.Cwd, pruneCwd) {
			continue
		}
		if pruneModel != "" && !strings.Contains(session.NormalizeModelName(s.Model), pruneModel) {
			continue
		}
		files, size := reclaimableFiles(s)
		if len(files) == 0 {
			alreadyClean++
			continue
		}
		candidates = append(candidates, pruneCandidate{s: s, files: files, size: size})
	}

	// Oldest first for stable, scannable output.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].s.StartedAt.Before(candidates[j].s.StartedAt)
	})

	var reclaimable int64
	for _, c := range candidates {
		reclaimable += c.size
	}

	fmt.Printf("PRUNE %s  dry-run=%v\n", pruneOlderThan, pruneDryRun)
	fmt.Printf("candidates: %d\n", len(candidates))
	fmt.Printf("running skipped: %d\n", runningSkipped)
	fmt.Printf("too new skipped: %d\n", tooNewSkipped)
	fmt.Printf("already clean: %d\n", alreadyClean)
	fmt.Printf("reclaimable: %s\n", formatBytes(reclaimable))

	if len(candidates) > 0 {
		fmt.Println()
	}
	for _, c := range candidates {
		fmt.Printf("%s  %s  %s  model=%s  task=%s\n",
			c.s.ID,
			formatAge(time.Since(c.s.StartedAt)),
			formatBytes(c.size),
			session.NormalizeModelName(c.s.Model),
			truncateRunesASCII(singleLineTrimmed(c.s.Task), 50))
	}

	if pruneDryRun {
		return nil
	}

	var (
		pruned    int
		reclaimed int64
	)
	for _, c := range candidates {
		s := c.s
		// Make sure turns/cost survive log deletion. Trust an existing cache;
		// otherwise scan the log now, and skip the session if that fails.
		if _, statErr := os.Stat(s.LogFile); statErr == nil && !(s.StatsCached || s.Turns > 0) {
			if err := cacheSessionLogStats(s); err != nil {
				fmt.Fprintf(os.Stderr, "warn: skip %s: cache log stats: %v\n", s.ID, err)
				continue
			}
		}
		removeFailed := false
		for _, f := range c.files {
			info, statErr := os.Stat(f)
			if statErr != nil {
				continue
			}
			if err := os.Remove(f); err != nil {
				fmt.Fprintf(os.Stderr, "warn: remove %s: %v\n", f, err)
				removeFailed = true
				continue
			}
			reclaimed += info.Size()
		}
		if !removeFailed {
			pruned++
		}
	}

	fmt.Printf("\npruned %d sessions; reclaimed %s\n", pruned, formatBytes(reclaimed))
	return nil
}

// reclaimableFiles returns the bulky artifact files that currently exist for a
// session, plus their total size. Session JSON is never included.
func reclaimableFiles(s *session.Session) ([]string, int64) {
	var paths []string
	if s.LogFile != "" {
		paths = append(paths, s.LogFile, s.LogFile+".stderr", s.LogFile+".watch.log")
	}
	for _, p := range []string{s.ScriptFile, s.TaskFile, s.RuntimeFile, s.CancelFile} {
		if p != "" {
			paths = append(paths, p)
		}
	}

	var (
		files []string
		size  int64
	)
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, p)
		size += info.Size()
	}
	return files, size
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	f := float64(n)
	for _, u := range []string{"K", "M", "G", "T"} {
		f /= unit
		if f < unit || u == "T" {
			return fmt.Sprintf("%.1f%s", f, u)
		}
	}
	return fmt.Sprintf("%dB", n)
}

func formatAge(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return d.Round(time.Minute).String()
}
