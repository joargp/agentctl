// Package notify sends completion notifications for agentctl sessions.
// It currently supports:
//   - follow_up messages to a pi session control socket
//   - immediate event JSON files for external runtimes like Munin
package notify

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type sendCmd struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
}

type eventFile struct {
	Type      string            `json:"type"`
	ChannelID string            `json:"channelId"`
	Text      string            `json:"text"`
	ThreadTs  string            `json:"threadTs,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ImmediateEvent describes a file-based immediate event notification.
type ImmediateEvent struct {
	ChannelID string
	Text      string
	ThreadTs  string
	Metadata  map[string]string
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
	conn.Read(buf) // response is informational; ignore errors
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

// SocketExists reports whether the control socket for sessionID exists and is
// accepting connections.
func SocketExists(sessionID string) bool {
	conn, err := net.DialTimeout("unix", socketPath(sessionID), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func socketPath(sessionID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "session-control", sessionID+".sock")
}
