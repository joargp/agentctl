package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/render"
	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
	"github.com/spf13/cobra"
)

var streamCmd = &cobra.Command{
	Use:               "stream <id>",
	Short:             "Stream live formatted output from an agent session",
	ValidArgsFunction: completeSessionIDs,
	Long: `Tail a session's NDJSON log and render it as human-readable formatted output
with colors, icons, and Pi-TUI-like styling.

Similar to 'dump --follow' but with richer formatting that resembles the
standard Pi terminal interface.

Examples:
  agentctl stream abc123
  agentctl stream abc123 --no-color`,
	Args: cobra.ExactArgs(1),
	RunE: runStream,
}

var (
	streamNoColor bool
)

func init() {
	streamCmd.Flags().BoolVar(&streamNoColor, "no-color", false, "disable colored output")
	rootCmd.AddCommand(streamCmd)
}

func runStream(_ *cobra.Command, args []string) error {
	id := args[0]
	s, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session %s: %w", id, err)
	}

	// Set up renderer.
	var opts []render.Option
	if streamNoColor {
		opts = append(opts, render.WithNoColor())
	}
	renderer := render.New(os.Stdout, opts...)

	// Print session header.
	printStreamHeader(s, streamNoColor)

	// Tail the log file.
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // inotify unreliable on Docker bind mounts
		Location:  &tail.SeekInfo{Offset: 0, Whence: 0}, // start from beginning
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		return fmt.Errorf("tail %s: %w", s.LogFile, err)
	}
	defer t.Cleanup()

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	// Stop on Ctrl-C.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		select {
		case <-sig:
			closeDone()
		case <-done:
		}
	}()

	// Auto-stop when session ends.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !tmux.SessionExists(s.TmuxSession) {
					time.Sleep(500 * time.Millisecond) // let final events flush
					closeDone()
					return
				}
			}
		}
	}()

	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				return nil
			}
			if line.Err != nil {
				continue
			}
			renderer.RenderLine([]byte(line.Text))
		case <-done:
			// Drain remaining.
			for {
				select {
				case line, ok := <-t.Lines:
					if !ok || line == nil {
						goto finish
					}
					if line.Err != nil {
						continue
					}
					renderer.RenderLine([]byte(line.Text))
				default:
					goto finish
				}
			}
		}
	}
finish:
	// Print a summary footer.
	printStreamFooter(s, streamNoColor)
	return nil
}

func printStreamHeader(s *session.Session, noColor bool) {
	running := tmux.SessionExists(s.TmuxSession)
	statusLabel := "done"
	if running {
		statusLabel = "running"
	}
	task := truncateRunesASCII(singleLineTrimmed(s.Task), 80)
	if noColor {
		fmt.Printf("═══ %s  %s  %s\n", s.ID, s.Label(), statusLabel)
		fmt.Printf("    %s\n\n", task)
	} else {
		fmt.Printf("\033[1m═══ %s\033[0m  %s  \033[32m%s\033[0m\n", s.ID, s.Label(), statusLabel)
		fmt.Printf("\033[2m    %s\033[0m\n\n", task)
	}
}

func printStreamFooter(s *session.Session, noColor bool) {
	cost := extractTotalCost(s.LogFile)
	elapsed := time.Since(s.StartedAt).Round(time.Second)
	if noColor {
		fmt.Printf("\n═══ session complete · %s", elapsed)
		if cost > 0 {
			fmt.Printf(" · $%.4f", cost)
		}
		fmt.Print("\n")
	} else {
		fmt.Printf("\n\033[2m═══ session complete · %s", elapsed)
		if cost > 0 {
			fmt.Printf(" · $%.4f", cost)
		}
		fmt.Print("\033[0m\n")
	}
}
