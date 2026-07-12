package cmd

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joargp/agentctl/internal/session"
)

// makePruneFixture saves a session started `age` ago and writes fake log,
// stderr, script, and task files for it under the temp HOME data dir.
func makePruneFixture(t *testing.T, id string, age time.Duration, logContent string) *session.Session {
	t.Helper()

	s := &session.Session{
		ID:          id,
		Model:       "gpt-test",
		Task:        "prune fixture task",
		Cwd:         "/repos/fixture",
		TmuxSession: "agentctl-" + id,
		StartedAt:   time.Now().Add(-age),
	}
	// Save hydrates LogFile/ScriptFile/... under the current HOME.
	saveSessionForTest(t, s)

	writePruneFile(t, s.LogFile, logContent)
	writePruneFile(t, s.LogFile+".stderr", "stderr output")
	writePruneFile(t, s.ScriptFile, "#!/bin/sh\n")
	writePruneFile(t, s.TaskFile, "task text")
	return s
}

func writePruneFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// execPrune runs runPrune with the given flags and a stubbed tmux snapshot,
// restoring package state afterwards.
func execPrune(t *testing.T, running map[string]bool, olderThan string, dryRun bool) (string, error) {
	t.Helper()

	prevOlderThan, prevDryRun := pruneOlderThan, pruneDryRun
	prevCwd, prevModel := pruneCwd, pruneModel
	prevList := listTmuxSessionsForPrune
	defer func() {
		pruneOlderThan, pruneDryRun = prevOlderThan, prevDryRun
		pruneCwd, pruneModel = prevCwd, prevModel
		listTmuxSessionsForPrune = prevList
	}()
	pruneOlderThan = olderThan
	pruneDryRun = dryRun
	pruneCwd = ""
	pruneModel = ""
	if running == nil {
		running = map[string]bool{}
	}
	listTmuxSessionsForPrune = func() map[string]bool { return running }

	var err error
	out := captureStdout(t, func() {
		err = runPrune(nil, nil)
	})
	return out, err
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err: %v", path, err)
	}
}

func TestPruneDryRunDoesNotDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := makePruneFixture(t, "dryrun01", 40*24*time.Hour, `{"type":"turn_end"}`+"\n")

	out, err := execPrune(t, nil, "30d", true)
	if err != nil {
		t.Fatalf("prune dry-run returned error: %v", err)
	}

	mustExist(t, s.LogFile)
	mustExist(t, s.LogFile+".stderr")
	mustExist(t, s.ScriptFile)
	mustExist(t, s.TaskFile)

	if !strings.Contains(out, s.ID) {
		t.Fatalf("expected dry-run output to mention session id %q, got %q", s.ID, out)
	}
	if !strings.Contains(out, "candidates: 1") {
		t.Fatalf("expected 1 candidate, got %q", out)
	}
	if !strings.Contains(out, "reclaimable:") {
		t.Fatalf("expected reclaimable size in output, got %q", out)
	}
	if strings.Contains(out, "pruned ") {
		t.Fatalf("dry-run must not report pruned sessions, got %q", out)
	}
}

func TestPruneDeletesLogsKeepsSessionJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := makePruneFixture(t, "delete01", 40*24*time.Hour, `{"type":"turn_end"}`+"\n")
	s.Turns = 5
	s.TotalCost = 1.5
	s.StatsCached = true
	saveSessionForTest(t, s)

	out, err := execPrune(t, nil, "30d", false)
	if err != nil {
		t.Fatalf("prune returned error: %v", err)
	}

	mustNotExist(t, s.LogFile)
	mustNotExist(t, s.LogFile+".stderr")
	mustNotExist(t, s.ScriptFile)
	mustNotExist(t, s.TaskFile)

	loaded, err := session.Load(s.ID)
	if err != nil {
		t.Fatalf("session metadata must survive prune: %v", err)
	}
	if loaded.Task != s.Task || loaded.Model != s.Model {
		t.Fatalf("expected task/model to be kept, got %#v", loaded)
	}
	if loaded.Turns != 5 || math.Abs(loaded.TotalCost-1.5) > 0.000001 {
		t.Fatalf("expected cached turns/cost to be kept, got turns=%d cost=%f", loaded.Turns, loaded.TotalCost)
	}
	if !strings.Contains(out, "pruned 1 sessions") {
		t.Fatalf("expected prune summary, got %q", out)
	}
}

func TestPruneSkipsRunningSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := makePruneFixture(t, "running1", 40*24*time.Hour, `{"type":"turn_end"}`+"\n")

	out, err := execPrune(t, map[string]bool{s.TmuxSession: true}, "30d", false)
	if err != nil {
		t.Fatalf("prune returned error: %v", err)
	}

	mustExist(t, s.LogFile)
	mustExist(t, s.LogFile+".stderr")
	if !strings.Contains(out, "running skipped: 1") {
		t.Fatalf("expected running session to be skipped, got %q", out)
	}
	if !strings.Contains(out, "candidates: 0") {
		t.Fatalf("expected 0 candidates, got %q", out)
	}
}

func TestPruneSkipsRecentSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := makePruneFixture(t, "recent01", time.Hour, `{"type":"turn_end"}`+"\n")

	out, err := execPrune(t, nil, "30d", false)
	if err != nil {
		t.Fatalf("prune returned error: %v", err)
	}

	mustExist(t, s.LogFile)
	mustExist(t, s.ScriptFile)
	if !strings.Contains(out, "too new skipped: 1") {
		t.Fatalf("expected recent session to be skipped, got %q", out)
	}
	if !strings.Contains(out, "candidates: 0") {
		t.Fatalf("expected 0 candidates, got %q", out)
	}
}

func TestPruneIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := makePruneFixture(t, "idem0001", 40*24*time.Hour, `{"type":"turn_end"}`+"\n")

	if _, err := execPrune(t, nil, "30d", false); err != nil {
		t.Fatalf("first prune returned error: %v", err)
	}
	mustNotExist(t, s.LogFile)

	out, err := execPrune(t, nil, "30d", false)
	if err != nil {
		t.Fatalf("second prune returned error: %v", err)
	}
	if !strings.Contains(out, "candidates: 0") {
		t.Fatalf("expected 0 candidates on second run, got %q", out)
	}
	if !strings.Contains(out, "already clean: 1") {
		t.Fatalf("expected session to count as already clean, got %q", out)
	}
}

func TestPruneCachesStatsBeforeDeletingLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logContent := `{"type":"turn_end","message":{"usage":{"cost":{"total":0.05}}}}` + "\n"
	s := makePruneFixture(t, "cache001", 40*24*time.Hour, logContent)
	if s.Turns != 0 || s.StatsCached {
		t.Fatalf("fixture must start without cached stats, got %#v", s)
	}

	if _, err := execPrune(t, nil, "30d", false); err != nil {
		t.Fatalf("prune returned error: %v", err)
	}

	mustNotExist(t, s.LogFile)
	loaded, err := session.Load(s.ID)
	if err != nil {
		t.Fatalf("load session after prune: %v", err)
	}
	if loaded.Turns != 1 {
		t.Fatalf("expected 1 turn cached before log deletion, got %d", loaded.Turns)
	}
	if math.Abs(loaded.TotalCost-0.05) > 0.000001 {
		t.Fatalf("expected cost 0.05 cached before log deletion, got %f", loaded.TotalCost)
	}
	if !loaded.StatsCached {
		t.Fatal("expected StatsCached to be set after prune")
	}
}

func TestPruneRequiresOlderThan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := execPrune(t, nil, "", false); err == nil {
		t.Fatal("expected error when --older-than is missing")
	}
	if _, err := execPrune(t, nil, "notaduration", false); err == nil {
		t.Fatal("expected error for invalid --older-than value")
	}
}
