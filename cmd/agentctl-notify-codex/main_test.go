package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSkipsWhenThreadIDMissing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(
		t.Context(),
		strings.NewReader(`{"schemaVersion":1,"event":"session.completed","message":"done"}`),
		&stdout,
		&stderr,
		func(string) string { return "" },
	)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "CODEX_THREAD_ID is not set") {
		t.Fatalf("expected skip message on stderr, got %q", stderr.String())
	}
}

func TestRunSendsMessageToCodexAppServer(t *testing.T) {
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "calls.jsonl")
	fakeCodex := writeFakeCodex(t, dir, callsPath, false)

	var stdout, stderr bytes.Buffer
	payload := `{"schemaVersion":1,"event":"session.completed","message":"Agent finished."}`
	err := run(
		t.Context(),
		strings.NewReader(payload),
		&stdout,
		&stderr,
		func(key string) string {
			switch key {
			case "AGENTCTL_CODEX_BIN":
				return fakeCodex
			case "AGENTCTL_CODEX_THREAD_ID":
				return "thread-123"
			case "AGENTCTL_CODEX_TIMEOUT_SECONDS":
				return "2"
			default:
				return ""
			}
		},
	)
	if err != nil {
		t.Fatalf("run returned error: %v (stderr %q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "notified Codex thread thread-123") {
		t.Fatalf("expected success message, got stdout=%q", stdout.String())
	}

	lines := readJSONLines(t, callsPath)
	var sawResume, sawTurn bool
	for _, line := range lines {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("Unmarshal call returned error: %v", err)
		}
		switch msg["method"] {
		case "thread/resume":
			params := msg["params"].(map[string]any)
			if params["threadId"] == "thread-123" {
				sawResume = true
			}
		case "turn/start":
			params := msg["params"].(map[string]any)
			input := params["input"].([]any)
			first := input[0].(map[string]any)
			if params["threadId"] == "thread-123" && first["type"] == "text" && first["text"] == "Agent finished." {
				sawTurn = true
			}
		}
	}
	if !sawResume {
		t.Fatal("expected thread/resume call")
	}
	if !sawTurn {
		t.Fatal("expected turn/start call with payload message")
	}
}

func TestRunReturnsCodexTurnFailure(t *testing.T) {
	dir := t.TempDir()
	fakeCodex := writeFakeCodex(t, dir, filepath.Join(dir, "calls.jsonl"), fakeCodexOptions{failTurnStart: true})

	err := run(
		t.Context(),
		strings.NewReader(`{"schemaVersion":1,"event":"session.completed","message":"Agent finished."}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(key string) string {
			switch key {
			case "AGENTCTL_CODEX_BIN":
				return fakeCodex
			case "CODEX_THREAD_ID":
				return "thread-123"
			case "AGENTCTL_CODEX_TIMEOUT_SECONDS":
				return "2"
			default:
				return ""
			}
		},
	)
	if err == nil {
		t.Fatal("expected failed Codex turn to return error")
	}
	if !strings.Contains(err.Error(), "turn/start") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected turn/start failure in error, got %v", err)
	}
}

func TestRunDoesNotWaitForTurnCompletion(t *testing.T) {
	dir := t.TempDir()
	fakeCodex := writeFakeCodex(t, dir, filepath.Join(dir, "calls.jsonl"), fakeCodexOptions{omitTurnCompleted: true})

	err := run(
		t.Context(),
		strings.NewReader(`{"schemaVersion":1,"event":"session.completed","message":"Agent finished."}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(key string) string {
			switch key {
			case "AGENTCTL_CODEX_BIN":
				return fakeCodex
			case "CODEX_THREAD_ID":
				return "thread-123"
			case "AGENTCTL_CODEX_TIMEOUT_SECONDS":
				return "2"
			default:
				return ""
			}
		},
	)
	if err != nil {
		t.Fatalf("run returned error despite accepted turn/start: %v", err)
	}
}

func TestReadPayloadRejectsUnsupportedSchema(t *testing.T) {
	_, err := readPayload(strings.NewReader(`{"schemaVersion":2,"event":"session.completed","message":"done"}`))
	if err == nil {
		t.Fatal("expected unsupported schema to return error")
	}
}

type fakeCodexOptions struct {
	failTurnStart     bool
	omitTurnCompleted bool
}

func writeFakeCodex(t *testing.T, dir, callsPath string, opts any) string {
	t.Helper()
	options := fakeCodexOptions{}
	switch typed := opts.(type) {
	case bool:
		options.failTurnStart = typed
	case fakeCodexOptions:
		options = typed
	}
	turnStartResponse := `printf '{"id":3,"result":{"turn":{"id":"turn-123","items":[],"itemsView":"notLoaded","status":"inProgress","error":null}}}\n'`
	if options.failTurnStart {
		turnStartResponse = `printf '{"id":3,"error":{"code":123,"message":"boom"}}\n'`
	}
	turnCompleted := `printf '{"method":"turn/completed","params":{"threadId":"thread-123","turn":{"id":"turn-123","status":"completed","error":null}}}\n'`
	if options.omitTurnCompleted {
		turnCompleted = ``
	}
	path := filepath.Join(dir, "fake-codex.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$AGENTCTL_FAKE_CODEX_CALLS"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"id":1,"result":{}}\n'
      ;;
    *'"method":"thread/resume"'*)
      printf '{"id":2,"result":{"thread":{"id":"thread-123","status":{"type":"idle"},"turns":[]}}}\n'
      ;;
    *'"method":"turn/start"'*)
      ` + turnStartResponse + `
      ` + turnCompleted + `
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("AGENTCTL_FAKE_CODEX_CALLS", callsPath)
	return path
}

func readJSONLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
