# Autoresearch: agentctl progress reading

## Goal
Make `agentctl dump` and `agentctl monitor` return meaningful content while a pi agent is still running.

## Primary metric
`log_bytes_during_execution` — bytes of useful log content readable via `agentctl dump` while agent is running. Higher is better. Baseline is 0 (current TUI mode produces nothing capturable).

## Benchmark
`scripts/bench-progress.sh` spawns an agent, waits for output, measures readable bytes, then kills the agent.

## Key finding
`pi --mode json -p` produces streaming NDJSON output (not TUI) with events like `text_delta`, `tool_use_start`, `tool_result`, etc. This can be piped directly to a log file and parsed for progress.

## Approach
1. Change `agentctl run` to use `pi --mode json -p` instead of bare `pi -p`
2. Redirect stdout to a JSON log file instead of relying on pipe-pane
3. Update `dump` to parse JSON events for human-readable output
4. Update `monitor` to tail JSON events for streaming progress
