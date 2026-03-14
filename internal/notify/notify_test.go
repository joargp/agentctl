package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
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
