package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joargp/agentctl/internal/notify"
	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
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
)

func init() {
	watchCmd.Flags().StringVar(&watchNotifySession, "notify-session", "", "pi session ID to notify on completion")
	watchCmd.Flags().StringVar(&watchNotifyEventDir, "notify-event-dir", "", "directory to write a completion event JSON file to")
	watchCmd.Flags().StringVar(&watchNotifyEventChannel, "notify-event-channel", "", "channel ID to include in the completion event")
	watchCmd.Flags().StringVar(&watchNotifyEventThread, "notify-event-thread", "", "optional thread ts to include in the completion event")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(_ *cobra.Command, args []string) error {
	notifyOptions := watcherNotifyOptions{
		PiSessionID:  watchNotifySession,
		EventDir:     watchNotifyEventDir,
		EventChannel: watchNotifyEventChannel,
		EventThread:  watchNotifyEventThread,
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

	// Poll until the tmux session is gone.
	for tmux.SessionExists(s.TmuxSession) {
		time.Sleep(500 * time.Millisecond)
	}

	var errs []error
	message := completionMessage(s)

	if notifyOptions.PiSessionID != "" {
		if err := notify.SendFollowUp(notifyOptions.PiSessionID, message); err != nil {
			errs = append(errs, fmt.Errorf("pi session notify failed: %w", err))
		}
	}

	if notifyOptions.EventDir != "" {
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
