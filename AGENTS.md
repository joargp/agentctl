# agentctl

CLI for spawning and monitoring [pi](https://buildwithpi.ai) coding agent sessions via tmux. Written in Go.

## Architecture

```
main.go              → entry point, calls cmd.Execute()
cmd/                 → cobra commands (one file per command)
  root.go            → root command, data dir setup
  run.go             → spawn pi in a tmux session, write task to file, exec via shell script
  record.go          → stdin→stdout passthrough + sanitized NDJSON log (hidden, used internally)
  watch.go           → wait for session to finish, send completion notifications (hidden, internal)
  monitor.go         → live-stream labeled output from one or more sessions
  dump.go            → render session log as human-readable text (supports --follow, --json, --summary)
  status.go          → one-line activity summary for a session
  ls.go              → list sessions with filters (--model, --since, --running, --done, --cwd, --task)
  costs.go           → aggregate API costs across sessions
  kill.go            → kill tmux session(s), preserve logs
  attach.go          → attach terminal to a running tmux session
  session_stats.go   → cache turns/cost into session JSON after completion
  completion.go      → shell completion helpers
internal/
  session/
    session.go       → Session struct, CRUD (JSON files in ~/.local/share/agentctl/sessions/)
    recording.go     → SanitizeRecordingLine — strips quadratic accumulated-state snapshots from pi NDJSON
    batch.go         → delta event batching (merges consecutive toolcall_delta events in logs)
    activity.go      → parse pi events into human-readable activity states for status/progress
  notify/
    notify.go        → completion notifications: pi session follow_up (unix socket), event JSON files (Munin)
  tmux/
    tmux.go          → tmux wrapper: create/kill/attach sessions, capture pane output
scripts/
  install.sh         → install script (go install or GitHub release binary)
```

## Data flow

`agentctl run` creates a tmux session that runs:
```
pi --mode json --model <model> --no-session -p "$task" 2>log.stderr | agentctl record log.log
```
- Pi emits streaming NDJSON on stdout; stderr is redirected to a separate file (contains terminal escape sequences).
- `agentctl record` passes raw JSON to the terminal while writing a sanitized copy to the log file (strips large accumulated-state snapshots that would make logs grow quadratically).
- When `--notify-*` flags are set, a detached `agentctl watch` process polls for tmux session termination and sends notifications.

## Data storage

All state lives in `~/.local/share/agentctl/`:
- `sessions/<id>.json` — session metadata (model, task, cwd, timestamps, cached costs)
- `logs/<id>.log` — sanitized NDJSON event stream
- `logs/<id>.log.stderr` — pi stderr output
- `scripts/<id>.sh` — generated shell script that runs pi
- `scripts/<id>.task` — task prompt file (avoids shell quoting issues)

tmux socket: `$TMPDIR/claude-tmux-sockets/agentctl.sock`

## Key patterns

- **Cobra CLI**: each command in its own file in `cmd/`, registered via `init()`.
- **Session IDs**: 8-char hex (4 random bytes). tmux session names are `agentctl-<id>`.
- **Log sanitization**: `recording.go` strips `message`, `partial`, `toolCall`, and other accumulated-state fields from streaming events. `turn_end` is preserved (fires once per turn, used for summaries).
- **Delta batching**: consecutive `toolcall_delta` events are merged into a single log entry. `text_delta` and `thinking_delta` are NOT batched (needed for live rendering).
- **Notifications**: pi session follow_up via unix socket; Munin-compatible event JSON files (immediate + progress).

## Build & test

```bash
go build -o agentctl .       # build
go test ./...                 # run all tests
go install .                  # install to $GOPATH/bin
```

Tests exist for: `cmd/` (run, ls, status, monitor, dump, record, watch, cache), `internal/session/`, `internal/notify/`. No tests for `internal/tmux/` (requires live tmux).

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/nxadm/tail` — file tailing (used by dump --follow and watch progress)
- Requires `tmux` and `pi` in `$PATH` at runtime

## Conventions

- Stdout is for machine-readable output (session IDs, rendered logs); stderr is for hints and diagnostics.
- Hidden commands (`record`, `watch`) are internal plumbing — not for direct user invocation.
- Shell quoting is handled by writing task content to files rather than passing through shell arguments.
