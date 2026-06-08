// Package notify sends completion notifications for agentctl sessions.
// It currently supports:
//   - follow_up messages to a pi session control socket
//   - immediate event JSON files for external runtimes like Munin
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const commandNotifierTimeout = 120 * time.Second
const commandNotifierOutputLimit = 4096

type sendCmd struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
}

type eventFile struct {
	Type       string            `json:"type"`
	ChannelID  string            `json:"channelId"`
	Text       string            `json:"text"`
	ThreadTs   string            `json:"threadTs,omitempty"`
	SubagentID string            `json:"subagentId,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Replace    bool              `json:"replace,omitempty"`
}

// CompletionCommandPayload is the stable JSON payload sent to executable
// completion notifiers over stdin.
type CompletionCommandPayload struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Event         string                   `json:"event"`
	Session       CompletionCommandSession `json:"session"`
	Message       string                   `json:"message"`
	DumpCommand   string                   `json:"dumpCommand"`
}

// CompletionCommandSession contains session metadata for command notifiers.
type CompletionCommandSession struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Model     string    `json:"model"`
	Task      string    `json:"task"`
	Cwd       string    `json:"cwd"`
	StartedAt time.Time `json:"startedAt"`
	LogFile   string    `json:"logFile"`
	Turns     int       `json:"turns"`
	TotalCost float64   `json:"totalCost"`
}

// ImmediateEvent describes a file-based immediate event notification.
type ImmediateEvent struct {
	ChannelID string
	Text      string
	ThreadTs  string
	Metadata  map[string]string
}

// ProgressEvent describes a file-based progress update notification.
type ProgressEvent struct {
	ChannelID  string
	ThreadTs   string
	SubagentID string
	Text       string
	Model      string // model used by the subagent (optional, included in first event)
	Name       string // short subagent name (optional, included in first event)
	Task       string // short task description (optional, included in first event)
	Category   string // progress category such as thinking, tool:bash, error
	Replace    bool   // replace the progress body with the provided text
}

// SendFollowUp delivers message to the pi session identified by sessionID as a
// follow_up (queued after the current turn). It returns an error if the socket
// is unreachable or the write fails.
func SendFollowUp(sessionID, message string) error {
	socketPath := socketPath(sessionID)
	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect to session socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	cmd := sendCmd{
		Type:    "send",
		Message: message,
		Mode:    "follow_up",
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	data = append(data, '\n')

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	// Read the response before closing so the server doesn't get EPIPE
	// trying to write its acknowledgement to an already-closed socket.
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf) // response is informational; ignore errors
	return nil
}

// WriteImmediateEvent writes an immediate event JSON file atomically.
func WriteImmediateEvent(dir string, event ImmediateEvent) error {
	if dir == "" {
		return fmt.Errorf("event dir is required")
	}
	if event.ChannelID == "" {
		return fmt.Errorf("event channel ID is required")
	}
	if event.Text == "" {
		return fmt.Errorf("event text is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create event dir: %w", err)
	}

	payload := eventFile{
		Type:      "immediate",
		ChannelID: event.ChannelID,
		Text:      event.Text,
		ThreadTs:  event.ThreadTs,
		Metadata:  event.Metadata,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')

	base := fmt.Sprintf("agentctl-done-%d-%d", time.Now().UnixNano(), os.Getpid())
	if err := writeEventFile(dir, base, data); err != nil {
		return fmt.Errorf("write immediate event: %w", err)
	}
	return nil
}

// WriteProgressEvent writes a progress event JSON file atomically.
func WriteProgressEvent(dir string, event ProgressEvent) error {
	if dir == "" {
		return fmt.Errorf("event dir is required")
	}
	if event.ChannelID == "" {
		return fmt.Errorf("event channel ID is required")
	}
	if event.SubagentID == "" {
		return fmt.Errorf("subagent ID is required")
	}
	if event.Text == "" {
		return fmt.Errorf("event text is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create event dir: %w", err)
	}

	payload := eventFile{
		Type:       "progress",
		ChannelID:  event.ChannelID,
		ThreadTs:   event.ThreadTs,
		SubagentID: event.SubagentID,
		Text:       event.Text,
		Replace:    event.Replace,
	}
	if event.Model != "" || event.Task != "" || event.Name != "" || event.Category != "" {
		payload.Metadata = make(map[string]string)
		if event.Model != "" {
			payload.Metadata["model"] = event.Model
		}
		if event.Name != "" {
			payload.Metadata["name"] = event.Name
		}
		if event.Task != "" {
			payload.Metadata["task"] = event.Task
		}
		if event.Category != "" {
			payload.Metadata["category"] = event.Category
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')

	base := fmt.Sprintf("progress-%s-%d-%d", event.SubagentID, time.Now().UnixNano(), os.Getpid())
	if err := writeEventFile(dir, base, data); err != nil {
		return fmt.Errorf("write progress event: %w", err)
	}
	return nil
}

// CleanupProgressFiles removes unread progress event files for the given subagent.
func CleanupProgressFiles(dir, subagentID string) error {
	if dir == "" || subagentID == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read event dir: %w", err)
	}

	prefix := "progress-" + subagentID + "-"
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !(strings.HasPrefix(name, prefix) || strings.HasPrefix(name, "."+prefix)) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SendCompletionCommand invokes an executable notifier with the completion
// payload on stdin. The command is executed directly without shell expansion.
func SendCompletionCommand(command string, payload CompletionCommandPayload) error {
	return SendCompletionCommandWithTimeout(command, payload, commandNotifierTimeout)
}

// SendCompletionCommands invokes every command and returns the joined failures.
func SendCompletionCommands(commands []string, payload CompletionCommandPayload) error {
	var errs []error
	for _, command := range commands {
		if err := SendCompletionCommand(command, payload); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SendCompletionCommandWithTimeout is exported for tests and callers that need
// tighter control over notifier runtime.
func SendCompletionCommandWithTimeout(command string, payload CompletionCommandPayload, timeout time.Duration) error {
	if err := ValidateCompletionCommand(command); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = commandNotifierTimeout
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal completion notifier payload: %w", err)
	}
	data = append(data, '\n')

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command)
	cmd.Stdin = bytes.NewReader(data)
	stdout := &limitedBuffer{limit: commandNotifierOutputLimit}
	stderr := &limitedBuffer{limit: commandNotifierOutputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("completion notifier %s timed out after %s%s", command, timeout, notifierOutputSuffix(stdout, stderr))
	}
	if err != nil {
		return fmt.Errorf("completion notifier %s failed: %w%s", command, err, notifierOutputSuffix(stdout, stderr))
	}
	return nil
}

// ValidateCompletionCommand ensures v1 notifier commands are explicit paths and
// not implicit PATH lookups or shell command strings.
func ValidateCompletionCommand(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("--notify-command cannot be empty")
	}
	if strings.ContainsRune(command, '\x00') {
		return fmt.Errorf("--notify-command cannot contain NUL bytes")
	}
	if strings.ContainsAny(command, " \t\r\n") {
		return fmt.Errorf("--notify-command must be a single executable path without arguments: %s", command)
	}
	if !strings.Contains(command, "/") && !strings.Contains(command, `\`) {
		return fmt.Errorf("--notify-command must be an explicit executable path, not a PATH lookup: %s", command)
	}
	return nil
}

func notifierOutputSuffix(stdout, stderr *limitedBuffer) string {
	var parts []string
	if out := strings.TrimSpace(stdout.String()); out != "" {
		parts = append(parts, "stdout: "+out)
	}
	if err := strings.TrimSpace(stderr.String()); err != "" {
		parts = append(parts, "stderr: "+err)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	s := b.buf.String()
	if b.truncated {
		s += "...[truncated]"
	}
	return s
}

var _ io.Writer = (*limitedBuffer)(nil)

func writeEventFile(dir, base string, data []byte) error {
	tmpPath := filepath.Join(dir, "."+base+".tmp")
	finalPath := filepath.Join(dir, base+".json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp event file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename event file: %w", err)
	}
	return nil
}

func socketPath(sessionID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "session-control", sessionID+".sock")
}
