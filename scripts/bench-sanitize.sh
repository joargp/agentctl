#!/usr/bin/env bash
# Benchmark: sanitization efficiency on real pi event streams.
# Uses existing large log files as a fixed 20K-line sample (no agent spawning).
# Primary metric: sanitized_bytes (lower = better).
set -euo pipefail
cd "$(dirname "$0")/.."

OUTPUT=$(go test -v -run TestBenchmarkSanitize ./internal/session/ -count=1 2>&1)
echo "$OUTPUT" >&2

# Extract the last occurrence of each METRIC line
SANITIZED=$(echo "$OUTPUT" | grep 'METRIC sanitized_bytes=' | tail -1 | sed 's/.*sanitized_bytes=//')
RAW=$(echo "$OUTPUT"       | grep 'METRIC raw_bytes='       | tail -1 | sed 's/.*raw_bytes=//')
PCT=$(echo "$OUTPUT"       | grep 'METRIC saved_pct='       | tail -1 | sed 's/.*saved_pct=//')

echo "METRIC sanitized_bytes=${SANITIZED:-0}"
echo "METRIC raw_bytes=${RAW:-0}"
echo "METRIC saved_pct=${PCT:-0}"
echo "${SANITIZED:-0}"
