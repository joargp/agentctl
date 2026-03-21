package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/joargp/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var recordCmd = &cobra.Command{
	Use:    "record <log-file>",
	Short:  "Record pi JSON output to a sanitized log file",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runRecord,
}

func init() {
	rootCmd.AddCommand(recordCmd)
}

func runRecord(_ *cobra.Command, args []string) error {
	logFile := args[0]
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", logFile, err)
	}
	defer f.Close()

	return recordStream(os.Stdin, os.Stdout, f)
}

// deltaBatch buffers consecutive delta events with the same key and merges
// their delta strings into a single event when flushed.
type deltaBatch struct {
	key   session.DeltaKey
	delta string // concatenated delta text
	event map[string]interface{}
}

func recordStream(in io.Reader, out io.Writer, log io.Writer) error {
	reader := bufio.NewReader(in)
	var batch *deltaBatch

	flushBatch := func() error {
		if batch == nil {
			return nil
		}
		line := session.MarshalBatchedDelta(batch.event, batch.delta)
		batch = nil
		if line == nil {
			return fmt.Errorf("marshal batched delta: internal error")
		}
		_, writeErr := log.Write(line)
		return writeErr
	}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Always pass through to terminal output (unmodified).
			if _, writeErr := out.Write(line); writeErr != nil {
				return writeErr
			}

			if !looksLikeJSON(line) {
				continue
			}

			sanitized := session.SanitizeRecordingLine(line)
			if !looksLikeJSON(sanitized) {
				continue
			}

			// Try to batch consecutive delta events.
			if key, delta, evt, ok := session.ParseBatchableDelta(sanitized); ok {
				if batch != nil && batch.key == key {
					// Extend current batch.
					batch.delta += delta
				} else {
					// Flush any previous batch, start a new one.
					if writeErr := flushBatch(); writeErr != nil {
						return writeErr
					}
					batch = &deltaBatch{key: key, delta: delta, event: evt}
				}
			} else {
				// Non-delta event: flush batch, write event directly.
				if writeErr := flushBatch(); writeErr != nil {
					return writeErr
				}
				if _, writeErr := log.Write(sanitized); writeErr != nil {
					return writeErr
				}
			}
		}
		if err != nil {
			// Flush remaining batch on EOF.
			if flushErr := flushBatch(); flushErr != nil {
				return flushErr
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// looksLikeJSON returns true if the line starts with '{' after trimming whitespace.
func looksLikeJSON(line []byte) bool {
	for _, b := range line {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}
