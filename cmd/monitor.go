package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
	"github.com/spf13/cobra"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor [id...]",
	Short: "Stream live output from one or more agent sessions",
	Long: `Tail the log files of one or more sessions with labeled, interleaved output.
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
				Logger:    tail.DiscardingLogger,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "tail %s: %v\n", s.ID, err)
				return
			}
			defer t.Cleanup()
			for l := range t.Lines {
				if l.Err != nil {
					continue
				}
				out <- line{label: label, text: l.Text}
			}
		}(s, label)
	}

	// Fail fast if every requested ID is invalid or not currently running.
	if len(entries) == 0 {
		return fmt.Errorf("no running sessions to monitor")
	}

	// Print until Ctrl-C
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(done)
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
