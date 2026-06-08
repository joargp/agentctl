package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCodexBin = "codex"
	defaultTimeout  = 8 * time.Second
)

type completionPayload struct {
	SchemaVersion int    `json:"schemaVersion"`
	Event         string `json:"event"`
	Message       string `json:"message"`
}

type rpcClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses map[int]chan rpcResponse
	events    chan rpcRequest
	errs      chan error
	mu        sync.Mutex
	nextID    int
	stderr    *bytes.Buffer
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     int             `json:"id,omitempty"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "agentctl-notify-codex: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, getenv func(string) string) error {
	payload, err := readPayload(stdin)
	if err != nil {
		return err
	}
	if payload.Event != "session.completed" {
		return nil
	}
	if strings.TrimSpace(payload.Message) == "" {
		return fmt.Errorf("payload message is required")
	}

	threadID := firstNonEmpty(getenv("AGENTCTL_CODEX_THREAD_ID"), getenv("CODEX_THREAD_ID"))
	if threadID == "" {
		fmt.Fprintln(stderr, "agentctl-notify-codex: CODEX_THREAD_ID is not set; skipping")
		return nil
	}

	timeout := timeoutFromEnv(getenv("AGENTCTL_CODEX_TIMEOUT_SECONDS"))
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	codexBin := firstNonEmpty(getenv("AGENTCTL_CODEX_BIN"), defaultCodexBin)
	client, err := startRPCClient(ctx, codexBin)
	if err != nil {
		return err
	}
	defer client.close()

	if err := client.initialize(ctx); err != nil {
		return err
	}
	if err := client.resumeThread(ctx, threadID); err != nil {
		return err
	}
	if err := client.startTurn(ctx, threadID, payload.Message); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "notified Codex thread %s\n", threadID)
	return nil
}

func readPayload(r io.Reader) (completionPayload, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return completionPayload{}, fmt.Errorf("read payload: %w", err)
	}
	var payload completionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return completionPayload{}, fmt.Errorf("decode payload: %w", err)
	}
	if payload.SchemaVersion != 1 {
		return completionPayload{}, fmt.Errorf("unsupported payload schemaVersion %d", payload.SchemaVersion)
	}
	return payload, nil
}

func timeoutFromEnv(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func startRPCClient(ctx context.Context, codexBin string) (*rpcClient, error) {
	cmd := exec.CommandContext(ctx, codexBin, "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex stdout: %w", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	client := &rpcClient{
		cmd:       cmd,
		stdin:     stdin,
		responses: make(map[int]chan rpcResponse),
		events:    make(chan rpcRequest, 32),
		errs:      make(chan error, 1),
		nextID:    1,
		stderr:    stderr,
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s app-server: %w", codexBin, err)
	}
	go client.readLoop(stdout)
	return client, nil
}

func (c *rpcClient) initialize(ctx context.Context) error {
	if _, err := c.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "agentctl_notify_codex",
			"title":   "agentctl Codex notifier",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *rpcClient) resumeThread(ctx context.Context, threadID string) error {
	_, err := c.call(ctx, "thread/resume", map[string]any{"threadId": threadID})
	return err
}

func (c *rpcClient) startTurn(ctx context.Context, threadID, message string) error {
	result, err := c.call(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input": []map[string]string{
			{"type": "text", "text": message},
		},
	})
	if err != nil {
		return err
	}
	var parsed struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return fmt.Errorf("decode turn/start result: %w", err)
	}
	if parsed.Turn.ID == "" {
		return fmt.Errorf("turn/start response did not include turn id")
	}
	return nil
}

func (c *rpcClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id, responseCh, err := c.send(method, params, true)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s request %d: %w", method, id, ctx.Err())
	case err := <-c.errs:
		return nil, err
	case response := <-responseCh:
		if response.Error != nil {
			return nil, fmt.Errorf("%s request %d failed: %s", method, id, response.Error.Message)
		}
		return response.Result, nil
	}
}

func (c *rpcClient) notify(method string, params any) error {
	_, _, err := c.send(method, params, false)
	return err
}

func (c *rpcClient) send(method string, params any, wantsResponse bool) (int, chan rpcResponse, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal %s params: %w", method, err)
	}

	c.mu.Lock()
	id := 0
	var ch chan rpcResponse
	if wantsResponse {
		id = c.nextID
		c.nextID++
		ch = make(chan rpcResponse, 1)
		c.responses[id] = ch
	}
	msg := rpcRequest{Method: method, Params: data, ID: id}
	wire, err := json.Marshal(msg)
	if err == nil {
		_, err = fmt.Fprintf(c.stdin, "%s\n", wire)
	}
	if err != nil && wantsResponse {
		delete(c.responses, id)
	}
	c.mu.Unlock()
	if err != nil {
		return 0, nil, fmt.Errorf("send %s: %w", method, err)
	}
	return id, ch, nil
}

func (c *rpcClient) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err == nil && response.ID != 0 {
			c.mu.Lock()
			ch := c.responses[response.ID]
			delete(c.responses, response.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- response
			}
			continue
		}
		var event rpcRequest
		if err := json.Unmarshal(line, &event); err == nil && event.Method != "" {
			c.events <- event
		}
	}
	if err := scanner.Err(); err != nil {
		c.errs <- fmt.Errorf("read codex app-server output: %w", err)
		return
	}
	c.errs <- errors.New("codex app-server exited before notification completed")
}

func (c *rpcClient) close() {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}
