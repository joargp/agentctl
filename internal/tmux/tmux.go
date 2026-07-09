package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SocketPath is the tmux socket used for all agentctl sessions.
var SocketPath string

func init() {
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	socketDir := os.Getenv("CLAUDE_TMUX_SOCKET_DIR")
	if socketDir == "" {
		socketDir = filepath.Join(tmp, "claude-tmux-sockets")
	}
	SocketPath = filepath.Join(socketDir, "agentctl.sock")
}

// EnsureSocket creates the socket directory if needed.
func EnsureSocket() error {
	return os.MkdirAll(filepath.Dir(SocketPath), 0o755)
}

func run(args ...string) error {
	full := append([]string{"-S", SocketPath}, args...)
	out, err := exec.Command("tmux", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %v: %w: %s", args, err, out)
	}
	return nil
}

// NewSession creates a detached tmux session running the given command.
// When the command exits the session is destroyed automatically.
func NewSession(name string, command ...string) error {
	args := []string{"new-session", "-d", "-s", name}
	args = append(args, command...)
	return run(args...)
}

// SessionExists reports whether the named session is alive.
func SessionExists(name string) bool {
	full := []string{"-S", SocketPath, "has-session", "-t", name}
	return exec.Command("tmux", full...).Run() == nil
}

// ListSessions returns the currently live tmux session names as a membership
// set. It mirrors SessionExists failure semantics: if tmux is missing, the
// server/socket does not exist, or there are no sessions, callers get an empty
// set rather than a fatal error.
func ListSessions() map[string]bool {
	full := []string{"-S", SocketPath, "list-sessions", "-F", "#{session_name}"}
	out, err := exec.Command("tmux", full...).Output()
	if err != nil {
		return map[string]bool{}
	}
	return parseSessionListOutput(out)
}

func parseSessionListOutput(out []byte) map[string]bool {
	sessions := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		sessions[name] = true
	}
	return sessions
}

// KillSession destroys a session.
func KillSession(name string) error {
	return run("kill-session", "-t", name)
}

// Attach hands the terminal over to the tmux session (replaces current process).
func Attach(name string) error {
	c := exec.Command("tmux", "-S", SocketPath, "attach", "-t", name)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
