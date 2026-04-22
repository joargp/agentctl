package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveHydratesDerivedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := &Session{
		ID:        "paths123",
		Model:     "openai/gpt-5.4",
		Task:      "test",
		Cwd:       "/tmp/project",
		StartedAt: time.Now(),
	}
	if err := Save(s); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	base := filepath.Join(home, ".local", "share", "agentctl")
	if s.LogFile != filepath.Join(base, "logs", "paths123.log") {
		t.Fatalf("unexpected log file path: %q", s.LogFile)
	}
	if s.ScriptFile != filepath.Join(base, "scripts", "paths123.sh") {
		t.Fatalf("unexpected script file path: %q", s.ScriptFile)
	}
	if s.TaskFile != filepath.Join(base, "scripts", "paths123.task") {
		t.Fatalf("unexpected task file path: %q", s.TaskFile)
	}
	if s.RuntimeFile != filepath.Join(base, "runtime", "paths123.json") {
		t.Fatalf("unexpected runtime file path: %q", s.RuntimeFile)
	}
	if s.CancelFile != filepath.Join(base, "runtime", "paths123.cancelled") {
		t.Fatalf("unexpected cancel file path: %q", s.CancelFile)
	}

	loaded, err := Load("paths123")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.RuntimeFile != s.RuntimeFile {
		t.Fatalf("expected runtime file %q, got %q", s.RuntimeFile, loaded.RuntimeFile)
	}
	if loaded.CancelFile != s.CancelFile {
		t.Fatalf("expected cancel file %q, got %q", s.CancelFile, loaded.CancelFile)
	}
}
