package tmux

import "testing"

func TestParseSessionListOutputEmpty(t *testing.T) {
	sessions := parseSessionListOutput([]byte("\n\n"))
	if len(sessions) != 0 {
		t.Fatalf("expected empty session set, got %#v", sessions)
	}
}

func TestParseSessionListOutputMultipleLines(t *testing.T) {
	sessions := parseSessionListOutput([]byte("agentctl-abc12345\nagentctl-def67890\n"))
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %#v", sessions)
	}
	if !sessions["agentctl-abc12345"] || !sessions["agentctl-def67890"] {
		t.Fatalf("expected both parsed sessions, got %#v", sessions)
	}
}
