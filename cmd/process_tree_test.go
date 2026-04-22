package cmd

import (
	"io"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestTerminateProcessGroupKillsLiveTree(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30 & wait")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start shell: %v", err)
	}
	pgid := cmd.Process.Pid
	defer func() {
		_ = terminateProcessGroup(pgid, 250*time.Millisecond)
		_ = cmd.Wait()
	}()

	time.Sleep(100 * time.Millisecond)
	if !processGroupExists(pgid) {
		t.Fatalf("expected process group %d to exist", pgid)
	}

	if err := terminateProcessGroup(pgid, 500*time.Millisecond); err != nil {
		t.Fatalf("terminateProcessGroup returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for leader process to exit")
	}

	if processGroupExists(pgid) {
		t.Fatalf("expected process group %d to be gone", pgid)
	}
}
