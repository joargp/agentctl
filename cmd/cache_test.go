package cmd

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joargp/agentctl/internal/session"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = prev
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(out)
}

func writeSessionLog(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "session.log")
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return path
}

func saveSessionForTest(t *testing.T, s *session.Session) {
	t.Helper()
	if err := session.Save(s); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func TestGetSessionLogStatsUsesCachedValuesForCompletedSession(t *testing.T) {
	s := &session.Session{
		LogFile:   "/nonexistent/session.log",
		Turns:     3,
		TotalCost: 0.03,
	}

	stats := getSessionLogStats(s, false)
	if stats.Turns != 3 {
		t.Fatalf("expected cached turns, got %d", stats.Turns)
	}
	if math.Abs(stats.TotalCost-0.03) > 0.000001 {
		t.Fatalf("expected cached total cost, got %f", stats.TotalCost)
	}
}

func TestGetSessionLogStatsFallsBackToLogWhenCacheMissing(t *testing.T) {
	logFile := writeSessionLog(t,
		`{"type":"turn_end","message":{"usage":{"cost":{"total":0.01}}}}`,
		`{"type":"turn_end","message":{"usage":{"cost":{"total":0.02}}}}`,
	)
	s := &session.Session{LogFile: logFile}

	stats := getSessionLogStats(s, false)
	if stats.Turns != 2 {
		t.Fatalf("expected 2 turns from log, got %d", stats.Turns)
	}
	if math.Abs(stats.TotalCost-0.03) > 0.000001 {
		t.Fatalf("expected total cost ~0.03 from log, got %f", stats.TotalCost)
	}
}

func TestRunWatchCachesSessionStatsAfterExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logFile := writeSessionLog(t,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"done"}`,
		`{"type":"text_end","contentIndex":0,"content":"done"}`,
		`{"type":"turn_end","message":{"usage":{"cost":{"total":0.01}}}}`,
		`{"type":"turn_end","message":{"usage":{"cost":{"total":0.02}}}}`,
	)

	s := &session.Session{
		ID:          "watchcache",
		Model:       "gpt-test",
		Task:        "cache completed session stats",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     logFile,
		StartedAt:   time.Now().Add(-time.Minute),
	}
	saveSessionForTest(t, s)

	prevSession := watchNotifySession
	prevEventDir := watchNotifyEventDir
	prevEventChannel := watchNotifyEventChannel
	prevEventThread := watchNotifyEventThread
	prevProgress := watchProgress
	defer func() {
		watchNotifySession = prevSession
		watchNotifyEventDir = prevEventDir
		watchNotifyEventChannel = prevEventChannel
		watchNotifyEventThread = prevEventThread
		watchProgress = prevProgress
	}()

	watchNotifySession = ""
	watchNotifyEventDir = t.TempDir()
	watchNotifyEventChannel = "C123"
	watchNotifyEventThread = ""
	watchProgress = false

	if err := runWatch(nil, []string{s.ID}); err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}

	loaded, err := session.Load(s.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.Turns != 2 {
		t.Fatalf("expected cached turns=2, got %d", loaded.Turns)
	}
	if math.Abs(loaded.TotalCost-0.03) > 0.000001 {
		t.Fatalf("expected cached total cost ~0.03, got %f", loaded.TotalCost)
	}
}

func TestRunLsUsesCachedStatsForCompletedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := &session.Session{
		ID:          "lscache",
		Name:        "cached",
		Model:       "gpt-test",
		Task:        "show cached stats",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     "/nonexistent/session.log",
		StartedAt:   time.Now().Add(-time.Minute),
		Turns:       3,
		TotalCost:   0.03,
	}
	saveSessionForTest(t, s)

	prevModel, prevSince := lsModel, lsSince
	prevTask, prevCwd := lsTask, lsCwd
	prevRunning, prevDone, prevQuiet := lsRunning, lsDone, lsQuiet
	defer func() {
		lsModel, lsSince = prevModel, prevSince
		lsTask, lsCwd = prevTask, prevCwd
		lsRunning, lsDone, lsQuiet = prevRunning, prevDone, prevQuiet
	}()
	lsModel = ""
	lsSince = ""
	lsTask = ""
	lsCwd = ""
	lsRunning = false
	lsDone = false
	lsQuiet = false

	out := captureStdout(t, func() {
		if err := runLs(nil, nil); err != nil {
			t.Fatalf("runLs returned error: %v", err)
		}
	})

	if !strings.Contains(out, s.ID) {
		t.Fatalf("expected ls output to contain session id, got %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Fatalf("expected ls output to contain cached turns, got %q", out)
	}
	if !strings.Contains(out, "$0.0300") {
		t.Fatalf("expected ls output to contain cached cost, got %q", out)
	}
}

func TestPrintSessionStatusUsesCachedStatsForCompletedSession(t *testing.T) {
	s := &session.Session{
		ID:          "statuscache",
		Name:        "cached",
		Model:       "gpt-test",
		TmuxSession: "definitely-not-running",
		LogFile:     "/nonexistent/session.log",
		StartedAt:   time.Now().Add(-time.Minute),
		Turns:       3,
		TotalCost:   0.03,
	}

	out := captureStdout(t, func() {
		printSessionStatus(s)
	})

	if !strings.Contains(out, "3 turns") {
		t.Fatalf("expected status output to contain cached turns, got %q", out)
	}
	if !strings.Contains(out, "$0.0300") {
		t.Fatalf("expected status output to contain cached cost, got %q", out)
	}
}

func TestRunCostsUsesCachedCostForCompletedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := &session.Session{
		ID:          "costcache",
		Name:        "cached",
		Model:       "gpt-test",
		Task:        "show cached cost",
		Cwd:         home,
		TmuxSession: "definitely-not-running",
		LogFile:     "/nonexistent/session.log",
		StartedAt:   time.Now().Add(-time.Minute),
		Turns:       2,
		TotalCost:   0.03,
	}
	saveSessionForTest(t, s)

	prevSince := costsSince
	defer func() {
		costsSince = prevSince
	}()
	costsSince = ""

	out := captureStdout(t, func() {
		if err := runCosts(nil, nil); err != nil {
			t.Fatalf("runCosts returned error: %v", err)
		}
	})

	if !strings.Contains(out, s.ID) {
		t.Fatalf("expected costs output to contain session id, got %q", out)
	}
	if got := strings.Count(out, "$0.0300"); got < 2 {
		t.Fatalf("expected costs output to contain cached per-session and total cost, got %q", out)
	}
	if !strings.Contains(out, "TOTAL") {
		t.Fatalf("expected costs output to contain total row, got %q", out)
	}
}
