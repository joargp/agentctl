package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joargp/agentctl/internal/notify"
	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:    "watch <id>",
	Short:  "Wait for a session to finish then send completion notifications (internal)",
	Hidden: true, // not for direct use; spawned by 'run --notify-*'
	Args:   cobra.ExactArgs(1),
	RunE:   runWatch,
}

var (
	watchNotifySession      string
	watchNotifyEventDir     string
	watchNotifyEventChannel string
	watchNotifyEventThread  string
	watchProgress           bool
)

func init() {
	watchCmd.Flags().StringVar(&watchNotifySession, "notify-session", "", "pi session ID to notify on completion")
	watchCmd.Flags().StringVar(&watchNotifyEventDir, "notify-event-dir", "", "directory to write a completion event JSON file to")
	watchCmd.Flags().StringVar(&watchNotifyEventChannel, "notify-event-channel", "", "channel ID to include in the completion event")
	watchCmd.Flags().StringVar(&watchNotifyEventThread, "notify-event-thread", "", "optional thread ts to include in the completion event")
	watchCmd.Flags().BoolVar(&watchProgress, "progress", false, "emit progress events while waiting for completion")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(_ *cobra.Command, args []string) error {
	notifyOptions := watcherNotifyOptions{
		PiSessionID:  watchNotifySession,
		EventDir:     watchNotifyEventDir,
		EventChannel: watchNotifyEventChannel,
		EventThread:  watchNotifyEventThread,
		Progress:     watchProgress,
	}
	if err := validateWatcherNotifyOptions(notifyOptions); err != nil {
		return err
	}
	if !hasWatcherNotifications(notifyOptions) {
		return fmt.Errorf("watch requires at least one completion notifier")
	}

	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("load session %s: %w", id, err)
	}

	var progress *progressTailer
	if notifyOptions.Progress && notifyOptions.EventDir != "" {
		progress, err = startProgressTailer(s, notifyOptions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agentctl watch: progress tailer failed: %v\n", err)
		}
	}

	// Poll until the tmux session is gone.
	for tmux.SessionExists(s.TmuxSession) {
		time.Sleep(500 * time.Millisecond)
	}

	if progress != nil {
		progress.Stop()
	}

	var errs []error
	message := completionMessage(s)

	if notifyOptions.PiSessionID != "" {
		if err := notify.SendFollowUp(notifyOptions.PiSessionID, message); err != nil {
			errs = append(errs, fmt.Errorf("pi session notify failed: %w", err))
		}
	}

	if notifyOptions.EventDir != "" {
		if err := notify.CleanupProgressFiles(notifyOptions.EventDir, s.ID); err != nil {
			errs = append(errs, fmt.Errorf("cleanup progress files failed: %w", err))
		}

		metadata := map[string]string{
			"source": "agentctl",
			"event":  "subagent_done",
			"id":     s.ID,
			"model":  s.Model,
			"task":   s.Task,
			"cwd":    s.Cwd,
		}
		if s.Name != "" {
			metadata["name"] = s.Name
		}

		event := notify.ImmediateEvent{
			ChannelID: notifyOptions.EventChannel,
			ThreadTs:  notifyOptions.EventThread,
			Text:      "[AGENTCTL_DONE]\n" + message,
			Metadata:  metadata,
		}
		if err := notify.WriteImmediateEvent(notifyOptions.EventDir, event); err != nil {
			errs = append(errs, fmt.Errorf("event notify failed: %w", err))
		}
	}

	if len(errs) > 0 {
		err := errors.Join(errs...)
		fmt.Fprintf(os.Stderr, "agentctl watch: notify failed: %v\n", err)
		os.Exit(1)
	}
	return nil
}

type progressTailer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func startProgressTailer(s *session.Session, opts watcherNotifyOptions) (*progressTailer, error) {
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // inotify unreliable on Docker bind mounts
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("tail %s: %w", s.LogFile, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pt := &progressTailer{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go pt.run(ctx, t, s, opts)
	return pt, nil
}

func (p *progressTailer) Stop() {
	if p == nil {
		return
	}
	p.cancel()
	<-p.done
}

func (p *progressTailer) run(ctx context.Context, t *tail.Tail, s *session.Session, opts watcherNotifyOptions) {
	defer close(p.done)
	defer t.Cleanup()

	lastStatus := ""
	turnCount := 0
	draining := false

	for {
		if draining {
			select {
			case line, ok := <-t.Lines:
				if !ok {
					return
				}
				emitProgressLine(line, s, opts, &turnCount, &lastStatus)
			default:
				return
			}
			continue
		}

		select {
		case <-ctx.Done():
			draining = true
			_ = t.Stop()
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			emitProgressLine(line, s, opts, &turnCount, &lastStatus)
		}
	}
}

func emitProgressLine(line *tail.Line, s *session.Session, opts watcherNotifyOptions, turnCount *int, lastStatus *string) {
	if line == nil || line.Err != nil {
		return
	}

	activity := session.ParseActivityLine(line.Text, turnCount)
	if activity.Status == "" || activity.Status == *lastStatus {
		return
	}

	isFirst := *lastStatus == ""
	*lastStatus = activity.Status
	event := notify.ProgressEvent{
		ChannelID:  opts.EventChannel,
		ThreadTs:   opts.EventThread,
		SubagentID: s.ID,
		Text:       activity.Status,
		Replace:    activity.Replace,
	}
	// Include model and task in the first progress event so
	// the Munin runtime can display them in the progress header.
	if isFirst {
		event.Model = s.Model
		event.Task = truncateTask(s.Task, 100)
	}
	_ = notify.WriteProgressEvent(opts.EventDir, event)
}

func truncateTask(task string, maxLen int) string {
	task = strings.TrimSpace(task)
	// Take only the first line for display purposes
	if idx := strings.IndexAny(task, "\n\r"); idx >= 0 {
		task = task[:idx]
	}
	if len(task) > maxLen {
		return task[:maxLen-3] + "..."
	}
	return task
}

func completionMessage(s *session.Session) string {
	task := s.Task
	if len(task) > 80 {
		task = task[:77] + "..."
	}

	msg := fmt.Sprintf(
		"Agent **%s** (`%s`) finished.\nTask: %s\n",
		s.Model, s.ID, task,
	)

	// Try to include last few lines of rendered output as a summary.
	// Use readTail for performance on large log files.
	data := readTail(s.LogFile, 128*1024)
	if len(data) > 0 {
		rendered := renderJSONLog(data)
		lines := splitLines([]byte(rendered))
		// Take last 20 non-empty lines as summary
		var summary []string
		for i := len(lines) - 1; i >= 0 && len(summary) < 20; i-- {
			if lines[i] != "" {
				summary = append([]string{lines[i]}, summary...)
			}
		}
		if len(summary) > 0 {
			msg += "\n**Output (last lines):**\n```\n"
			for _, l := range summary {
				msg += l + "\n"
			}
			msg += "```\n"
		}
	}

	msg += fmt.Sprintf("\nRun `agentctl dump %s` for the full output.", s.ID)
	return msg
}
