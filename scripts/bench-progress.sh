#!/bin/bash
# Benchmark: measures bytes of readable progress during agent execution
# Spawns an agent, waits, checks dump output, then kills it.
# Exits with code 0 and prints the byte count.
set -euo pipefail

cd "$(dirname "$0")/.."

# Build first
go build -o agentctl .
# Also install to PATH so spawned scripts use the new binary
go install .

# Spawn agent with a task that takes a while
export PI_SESSION_ID=""
ID=$(./agentctl run --model claude-opus-4-6 --name bench --task "Use bash to run 'echo step1' then 'echo step2' then 'echo step3'. After each, write a paragraph about what you did." --cwd /tmp 2>/dev/null)

echo "Session: $ID" >&2

# Wait for agent to start producing output
sleep 15

# Measure progress bytes from dump
DUMP_OUTPUT=$(./agentctl dump "$ID" -n 500 2>/dev/null || true)
DUMP_BYTES=$(echo -n "$DUMP_OUTPUT" | wc -c | tr -d ' ')

# Also check log file size
HOME_DIR=$(eval echo ~)
LOG_FILE="$HOME_DIR/.local/share/agentctl/logs/$ID.log"
LOG_BYTES=$(wc -c < "$LOG_FILE" 2>/dev/null | tr -d ' ' || echo 0)

echo "dump_bytes=$DUMP_BYTES log_bytes=$LOG_BYTES" >&2

# Kill the agent
./agentctl kill "$ID" 2>/dev/null || true

# Output the metric (dump bytes - the key metric for readable progress)
echo "$DUMP_BYTES"
