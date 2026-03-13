// Package notify sends a follow_up message to a pi session's control socket.
// The socket lives at ~/.pi/session-control/<session-id>.sock and accepts
// newline-delimited JSON commands as defined by the pi control.ts extension.
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

	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write command: %w", err)
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
