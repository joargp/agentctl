package session

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLogForTest(t *testing.T, home, id string, lines ...string) string {
	t.Helper()
	logDir := filepath.Join(home, ".local", "share", "agentctl", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	path := filepath.Join(logDir, id+".log")
	data := []byte("")
	for _, line := range lines {
		data = append(data, []byte(line+"\n")...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return path
}

func TestLoadRecoversSessionFromLogWhenJSONMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeLogForTest(t, home, "recoverme",
		`{"type":"session","timestamp":"2026-04-16T08:35:02.894Z","cwd":"/tmp/project"}`,
		`{"type":"message_start","message":{"role":"user","timestamp":1776328503123,"content":[{"type":"text","text":"review this screenshot"}]}}`,
		`{"type":"message_start","message":{"role":"assistant","model":"gemini-3.1-pro-preview","timestamp":1776328503194}}`,
		`{"type":"turn_end","message":{"model":"gemini-3.1-pro-preview","usage":{"cost":{"total":0.0618}}}}`,
	)

	s, err := Load("recoverme")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ID != "recoverme" {
		t.Fatalf("expected recovered id, got %q", s.ID)
	}
	if s.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("expected recovered model, got %q", s.Model)
	}
	if s.Task != "review this screenshot" {
		t.Fatalf("expected recovered task, got %q", s.Task)
	}
	if s.Cwd != "/tmp/project" {
		t.Fatalf("expected recovered cwd, got %q", s.Cwd)
	}
	if s.StartedAt.IsZero() {
		t.Fatal("expected recovered started_at")
	}
	if s.Turns != 1 {
		t.Fatalf("expected recovered turns=1, got %d", s.Turns)
	}
	if math.Abs(s.TotalCost-0.0618) > 0.000001 {
		t.Fatalf("expected recovered cost, got %f", s.TotalCost)
	}
}

func TestListIncludesRecoveredLogOnlySessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	saved := &Session{
		ID:          "saved1234",
		Model:       "openai/gpt-5.4",
		Task:        "saved session",
		Cwd:         "/tmp/saved",
		TmuxSession: "agentctl-saved1234",
		LogFile:     filepath.Join(home, ".local", "share", "agentctl", "logs", "saved1234.log"),
		StartedAt:   mustParseTime(t, "2026-04-17T10:00:00Z"),
	}
	if err := Save(saved); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	writeLogForTest(t, home, "logonly01",
		`{"type":"session","timestamp":"2026-04-16T08:35:02.894Z","cwd":"/tmp/project"}`,
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"log only task"}]}}`,
		`{"type":"message_start","message":{"role":"assistant","model":"gemini-3.1-pro-preview"}}`,
	)

	sessions, err := List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	foundSaved := false
	foundRecovered := false
	for _, s := range sessions {
		switch s.ID {
		case "saved1234":
			foundSaved = true
		case "logonly01":
			foundRecovered = true
			if s.Model != "gemini-3.1-pro-preview" {
				t.Fatalf("expected recovered model, got %q", s.Model)
			}
		}
	}
	if !foundSaved || !foundRecovered {
		t.Fatalf("expected saved and recovered sessions, got %#v", sessions)
	}
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
