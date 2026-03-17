package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteImmediateEvent(t *testing.T) {
	dir := t.TempDir()
	err := WriteImmediateEvent(dir, ImmediateEvent{
		ChannelID: "C123",
		Text:      "[AGENTCTL_DONE]\nAgent finished.",
		ThreadTs:  "1710000000.000100",
		Metadata: map[string]string{
			"source": "agentctl",
			"id":     "abc123",
		},
	})
	if err != nil {
		t.Fatalf("WriteImmediateEvent returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 event file, got %d", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("expected json event file, got %s", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var payload struct {
		Type      string            `json:"type"`
		ChannelID string            `json:"channelId"`
		Text      string            `json:"text"`
		ThreadTs  string            `json:"threadTs"`
		Metadata  map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if payload.Type != "immediate" {
		t.Fatalf("expected type immediate, got %s", payload.Type)
	}
	if payload.ChannelID != "C123" {
		t.Fatalf("expected channel C123, got %s", payload.ChannelID)
	}
	if payload.ThreadTs != "1710000000.000100" {
		t.Fatalf("expected thread ts to round-trip, got %s", payload.ThreadTs)
	}
	if payload.Metadata["id"] != "abc123" {
		t.Fatalf("expected metadata id abc123, got %s", payload.Metadata["id"])
	}
}

func TestWriteImmediateEventRequiresFields(t *testing.T) {
	if err := WriteImmediateEvent("", ImmediateEvent{ChannelID: "C123", Text: "hello"}); err == nil {
		t.Fatal("expected error for missing dir")
	}
	if err := WriteImmediateEvent(t.TempDir(), ImmediateEvent{Text: "hello"}); err == nil {
		t.Fatal("expected error for missing channel")
	}
	if err := WriteImmediateEvent(t.TempDir(), ImmediateEvent{ChannelID: "C123"}); err == nil {
		t.Fatal("expected error for missing text")
	}
}

func TestWriteProgressEvent(t *testing.T) {
	dir := t.TempDir()
	err := WriteProgressEvent(dir, ProgressEvent{
		ChannelID:  "C123",
		ThreadTs:   "1710000000.000100",
		SubagentID: "abc123",
		Text:       "running `npm test`",
	})
	if err != nil {
		t.Fatalf("WriteProgressEvent returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 event file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "progress-abc123-") {
		t.Fatalf("expected filename to include subagent id, got %s", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var payload struct {
		Type       string `json:"type"`
		ChannelID  string `json:"channelId"`
		ThreadTs   string `json:"threadTs"`
		SubagentID string `json:"subagentId"`
		Text       string `json:"text"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if payload.Type != "progress" {
		t.Fatalf("expected type progress, got %s", payload.Type)
	}
	if payload.SubagentID != "abc123" {
		t.Fatalf("expected subagent id abc123, got %s", payload.SubagentID)
	}
	if payload.Text != "running `npm test`" {
		t.Fatalf("expected progress text to round-trip, got %q", payload.Text)
	}
}

func TestCleanupProgressFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"progress-abc123-1-1.json",
		"progress-abc123-2-2.json",
		"progress-other-3-3.json",
		"agentctl-done-4-4.json",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) returned error: %v", name, err)
		}
	}

	if err := CleanupProgressFiles(dir, "abc123"); err != nil {
		t.Fatalf("CleanupProgressFiles returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	remaining := make(map[string]bool, len(entries))
	for _, entry := range entries {
		remaining[entry.Name()] = true
	}
	if remaining["progress-abc123-1-1.json"] || remaining["progress-abc123-2-2.json"] {
		t.Fatalf("expected matching progress files to be removed, remaining=%v", remaining)
	}
	if !remaining["progress-other-3-3.json"] {
		t.Fatalf("expected other subagent progress file to remain, remaining=%v", remaining)
	}
	if !remaining["agentctl-done-4-4.json"] {
		t.Fatalf("expected done event to remain, remaining=%v", remaining)
	}
}
