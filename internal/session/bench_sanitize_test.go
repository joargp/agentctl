package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestBenchmarkSanitize reads a sample log file (specified by BENCH_LOG env var,
// or the first default path that exists) and reports sanitized vs raw bytes.
// Simulates the full recording pipeline: sanitize then batch consecutive deltas.
func TestBenchmarkSanitize(t *testing.T) {
	candidates := []string{
		os.Getenv("BENCH_LOG"),
		os.ExpandEnv("$HOME/.local/share/agentctl/logs/bd2646c5.log"),
		os.ExpandEnv("$HOME/.local/share/agentctl/logs/f3e79f7b.log"),
	}

	var logPath string
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			logPath = p
			break
		}
	}
	if logPath == "" {
		t.Skip("no sample log file found; set BENCH_LOG to a large agentctl log")
		return
	}

	const maxLines = 20_000

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var rawBytes int64
	var output bytes.Buffer
	linesRead := 0

	// Batching state (mirrors recordStream logic)
	type batch struct {
		key   DeltaKey
		delta string
		event map[string]interface{}
	}
	var current *batch

	flushBatch := func() {
		if current == nil {
			return
		}
		line := MarshalBatchedDelta(current.event, current.delta)
		current = nil
		if line != nil {
			output.Write(line)
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 16*1024*1024)
	for scanner.Scan() && linesRead < maxLines {
		line := append(scanner.Bytes(), '\n')
		rawBytes += int64(len(line))
		linesRead++

		sanitized := SanitizeRecordingLine(line)

		// Check if batchable
		if key, delta, evt, ok := ParseBatchableDelta(sanitized); ok {
			if current != nil && current.key == key {
				current.delta += delta
			} else {
				flushBatch()
				// Deep copy the event so batching doesn't share references.
				evtCopy := make(map[string]interface{})
				raw, _ := json.Marshal(evt)
				json.Unmarshal(raw, &evtCopy)
				current = &batch{key: key, delta: delta, event: evtCopy}
			}
		} else {
			flushBatch()
			output.Write(sanitized)
		}
	}
	flushBatch()

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	sanitizedBytes := int64(output.Len())
	saved := rawBytes - sanitizedBytes
	var savedPct float64
	if rawBytes > 0 {
		savedPct = float64(saved) / float64(rawBytes) * 100
	}

	fmt.Printf("Lines:           %d\n", linesRead)
	fmt.Printf("Raw bytes:       %d (%.1f KB)\n", rawBytes, float64(rawBytes)/1024)
	fmt.Printf("Sanitized bytes: %d (%.1f KB)\n", sanitizedBytes, float64(sanitizedBytes)/1024)
	fmt.Printf("Saved:           %d (%.1f%%)\n", saved, savedPct)
	fmt.Printf("METRIC sanitized_bytes=%d\n", sanitizedBytes)
	fmt.Printf("METRIC raw_bytes=%d\n", rawBytes)
	fmt.Printf("METRIC saved_pct=%.2f\n", savedPct)
}
