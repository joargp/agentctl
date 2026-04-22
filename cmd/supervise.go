package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/spf13/cobra"
)

var superviseCmd = &cobra.Command{
	Use:    "supervise <id>",
	Short:  "Run a session under process-tree supervision (internal)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runSupervise,
}

var superviseRender bool

func init() {
	superviseCmd.Flags().BoolVar(&superviseRender, "render", false, "render human-readable output instead of raw JSON")
	rootCmd.AddCommand(superviseCmd)
}

func runSupervise(_ *cobra.Command, args []string) error {
	s, err := session.Load(args[0])
	if err != nil {
		return fmt.Errorf("load session %s: %w", args[0], err)
	}
	return superviseSession(s, superviseRender)
}

func superviseSession(s *session.Session, render bool) error {
	if err := enableSubreaper(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not enable child subreaper: %v\n", err)
	}

	taskBytes, err := os.ReadFile(s.TaskFile)
	if err != nil {
		return fmt.Errorf("read task file %s: %w", s.TaskFile, err)
	}

	logWriter, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", s.LogFile, err)
	}
	defer logWriter.Close()

	stderrPath := s.LogFile + ".stderr"
	stderrWriter, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open stderr log %s: %w", stderrPath, err)
	}
	defer stderrWriter.Close()

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	defer pipeReader.Close()

	piCmd := exec.Command("pi", "--mode", "json", "--model", s.Model, "--no-session", "-p", string(taskBytes))
	piCmd.Dir = s.Cwd
	piCmd.Stdin = os.Stdin
	piCmd.Stdout = pipeWriter
	piCmd.Stderr = stderrWriter
	piCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := piCmd.Start(); err != nil {
		_ = pipeWriter.Close()
		return fmt.Errorf("start pi: %w", err)
	}
	_ = pipeWriter.Close()

	piErrCh := make(chan error, 1)
	go func() {
		piErrCh <- piCmd.Wait()
	}()

	pgid, err := syscall.Getpgid(piCmd.Process.Pid)
	if err != nil {
		_ = terminateProcessGroup(piCmd.Process.Pid, supervisorStopGrace)
		_, _ = waitForProcessWait(piErrCh, supervisorStopGrace)
		return fmt.Errorf("get pgid for pi pid %d: %w", piCmd.Process.Pid, err)
	}

	recorderErrCh := make(chan error, 1)
	go func() {
		if render {
			recorderErrCh <- recordStreamRendered(pipeReader, os.Stdout, logWriter)
			return
		}
		recorderErrCh <- recordStream(pipeReader, os.Stdout, logWriter)
	}()

	state := runtimeState{
		SessionID:     s.ID,
		SupervisorPID: os.Getpid(),
		LeaderPID:     piCmd.Process.Pid,
		PGID:          pgid,
	}

	if err := writeRuntimeState(s.RuntimeFile, state); err != nil {
		_ = terminateProcessGroup(state.PGID, supervisorStopGrace)
		_, _ = waitForProcessWait(piErrCh, supervisorStopGrace)
		_ = waitForRecorder(recorderErrCh, supervisorStopGrace)
		return fmt.Errorf("write runtime state %s: %w", s.RuntimeFile, err)
	}
	defer func() {
		_ = removeRuntimeState(s.RuntimeFile)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	var (
		piErr       error
		cleanupErrs []error
	)

	select {
	case piErr = <-piErrCh:
	case <-ctx.Done():
		cleanupErrs = append(cleanupErrs, fmt.Errorf("supervisor interrupted: %w", ctx.Err()))
	}

	if err := terminateProcessGroup(state.PGID, supervisorStopGrace); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	if piErr == nil {
		var waitTimedOut bool
		piErr, waitTimedOut = waitForProcessWait(piErrCh, supervisorStopGrace)
		if waitTimedOut {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("timed out waiting for pi (pid %d) to exit", state.LeaderPID))
		}
	}

	if recorderErr := waitForRecorder(recorderErrCh, supervisorStopGrace); recorderErr != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("record output: %w", recorderErr))
	}

	if err := reapOrphanedChildren(supervisorReapTimeout); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("reap orphaned children: %w", err))
	}

	if len(cleanupErrs) == 0 {
		return piErr
	}
	if piErr != nil {
		cleanupErrs = append([]error{piErr}, cleanupErrs...)
	}
	return errors.Join(cleanupErrs...)
}

func waitForProcessWait(result <-chan error, timeout time.Duration) (error, bool) {
	select {
	case err := <-result:
		return err, false
	case <-time.After(timeout):
		return nil, true
	}
}

func waitForRecorder(recorderErrCh <-chan error, timeout time.Duration) error {
	select {
	case err := <-recorderErrCh:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for recorder to finish")
	}
}
