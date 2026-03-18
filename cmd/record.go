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

func recordStream(in io.Reader, out io.Writer, log io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Always pass through to terminal output.
			if _, writeErr := out.Write(line); writeErr != nil {
				return writeErr
			}
			// Only write valid JSON lines to the log file.
			// Pi's stderr (merged via 2>&1) can contain terminal escape
			// sequences (e.g., OSC notifications) that are not JSON and
			// can be very large, polluting the NDJSON log.
			sanitized := session.SanitizeRecordingLine(line)
			if looksLikeJSON(sanitized) {
				if _, writeErr := log.Write(sanitized); writeErr != nil {
					return writeErr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// looksLikeJSON returns true if the line starts with '{' after trimming whitespace.
// This is a fast pre-check to avoid writing non-JSON lines (e.g., terminal escape
// sequences) to the NDJSON log file.
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
