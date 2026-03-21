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

func output(args ...string) (string, error) {
	full := append([]string{"-S", SocketPath}, args...)
	out, err := exec.Command("tmux", full...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %v: %w", args, err)
	}
	return string(out), nil
}

// NewSession creates a detached tmux session running the given command.
// When the command exits the session is destroyed automatically.
func NewSession(name string, command ...string) error {
	args := []string{"new-session", "-d", "-s", name}
	args = append(args, command...)
	return run(args...)
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PipePaneToFile streams pane output to a log file, stripping ANSI escape codes.
// logFile is shell-quoted so paths with spaces or metacharacters are safe.
func PipePaneToFile(session, logFile string) error {
	// sed strips ANSI/VT escape sequences so the log file is human-readable.
	stripCmd := `sed 's/\x1b\[[0-9;]*[mGKHFJABCDsuhl]//g; s/\x1b[^[]*//g; s/\r//g' >> ` + shellQuote(logFile)
	return run("pipe-pane", "-t", session, "-o", stripCmd)
}

// SendKeys sends a command string followed by Enter to the pane.
func SendKeys(session, cmd string) error {
	return run("send-keys", "-t", session, cmd, "Enter")
}

// SessionExists reports whether the named session is alive.
func SessionExists(name string) bool {
	full := []string{"-S", SocketPath, "has-session", "-t", name}
	return exec.Command("tmux", full...).Run() == nil
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

// CaptureLast returns the last n lines from the pane scrollback.
func CaptureLast(session string, lines int) (string, error) {
	return output("capture-pane", "-p", "-J", "-t", session, "-S", fmt.Sprintf("-%d", lines))
}
