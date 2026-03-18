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
			if _, writeErr := out.Write(line); writeErr != nil {
				return writeErr
			}
			if _, writeErr := log.Write(session.SanitizeRecordingLine(line)); writeErr != nil {
				return writeErr
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
