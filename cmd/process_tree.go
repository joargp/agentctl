package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/session"
)

const (
	runtimeCleanupGrace   = 2 * time.Second
	supervisorStopGrace   = 3 * time.Second
	supervisorReapTimeout = 3 * time.Second
)

type runtimeState struct {
	SessionID     string `json:"session_id"`
	SupervisorPID int    `json:"supervisor_pid"`
	LeaderPID     int    `json:"leader_pid"`
	PGID          int    `json:"pgid"`
}

func writeRuntimeState(path string, state runtimeState) error {
	if path == "" {
		return fmt.Errorf("runtime state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename runtime state: %w", err)
	}
	return nil
}

func readRuntimeState(path string) (*runtimeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state runtimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse runtime state %s: %w", path, err)
	}
	return &state, nil
}

func removeRuntimeState(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processGroupExists(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processExists(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return !processExists(pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func terminateProcessGroup(pgid int, grace time.Duration) error {
	if pgid <= 0 || !processGroupExists(pgid) {
		return nil
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("signal process group %d with SIGTERM: %w", pgid, err)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processGroupExists(pgid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("signal process group %d with SIGKILL: %w", pgid, err)
	}

	killDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(killDeadline) {
		if !processGroupExists(pgid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func reapOrphanedChildren(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		switch {
		case pid > 0:
			continue
		case err == nil && pid == 0:
			if time.Now().After(deadline) {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
			continue
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.ECHILD):
			return nil
		case err != nil:
			return err
		default:
			return nil
		}
	}
}

func processCommandLine(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func processLooksLikeSupervisor(pid int, sessionID string) bool {
	cmdline, err := processCommandLine(pid)
	if err != nil {
		return false
	}
	if cmdline == "" {
		return false
	}
	return strings.Contains(cmdline, "agentctl") && strings.Contains(cmdline, "supervise") && strings.Contains(cmdline, sessionID)
}

func processLooksLikePi(pid int) bool {
	cmdline, err := processCommandLine(pid)
	if err != nil {
		return false
	}
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return false
	}
	binary := filepath.Base(fields[0])
	return strings.Contains(binary, "pi") && strings.Contains(cmdline, "--mode json") && strings.Contains(cmdline, "--no-session")
}

func cleanupSessionProcessTree(s *session.Session, grace time.Duration) error {
	if s == nil || s.RuntimeFile == "" {
		return nil
	}

	state, err := readRuntimeState(s.RuntimeFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if state.SessionID != "" && state.SessionID != s.ID {
		return fmt.Errorf("runtime state %s belongs to session %s, not %s", s.RuntimeFile, state.SessionID, s.ID)
	}

	var errs []error
	if state.SupervisorPID > 0 && processLooksLikeSupervisor(state.SupervisorPID, s.ID) {
		// Ask the in-pane supervisor to perform its own graceful teardown first.
		// We may race with that cleanup here, so the follow-up PGID check below is
		// intentionally idempotent and only acts on the recorded process group.
		if err := syscall.Kill(state.SupervisorPID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("signal supervisor %d: %w", state.SupervisorPID, err))
		}
		if waitForProcessExit(state.SupervisorPID, grace) {
			if state.PGID > 0 && processGroupExists(state.PGID) {
				if err := terminateProcessGroup(state.PGID, grace); err != nil {
					errs = append(errs, err)
				}
			}
			if err := removeRuntimeState(s.RuntimeFile); err != nil {
				errs = append(errs, fmt.Errorf("remove runtime state: %w", err))
			}
			return errors.Join(errs...)
		}
	}

	if state.PGID > 0 && state.LeaderPID > 0 && processLooksLikePi(state.LeaderPID) {
		currentPGID, pgidErr := syscall.Getpgid(state.LeaderPID)
		if pgidErr == nil && currentPGID == state.PGID {
			if err := terminateProcessGroup(state.PGID, grace); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if state.SupervisorPID > 0 && processLooksLikeSupervisor(state.SupervisorPID, s.ID) && processExists(state.SupervisorPID) {
		if err := syscall.Kill(state.SupervisorPID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("kill supervisor %d: %w", state.SupervisorPID, err))
		}
		_ = waitForProcessExit(state.SupervisorPID, runtimeCleanupGrace)
	}

	if err := removeRuntimeState(s.RuntimeFile); err != nil {
		errs = append(errs, fmt.Errorf("remove runtime state: %w", err))
	}
	return errors.Join(errs...)
}
