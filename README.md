# agentctl

CLI for running and monitoring [pi](https://buildwithpi.ai) coding agent sessions. Spawn agents with any model, stream their output live, and watch multiple sessions side by side.

## Install

```bash
go install github.com/joargp/agentctl@latest
```

Or build from source:

```bash
git clone https://github.com/joargp/agentctl
cd agentctl
go install .
```

Requires `tmux` and `pi` in `$PATH`.

## Usage

### Spawn an agent

```bash
# Fire and forget — capture the ID
id=$(agentctl run --model claude-sonnet-4-6 --task "add unit tests to the auth module" --cwd /repos/myapp 2>/dev/null)

# Or pass task from file (safer for large prompts)
id=$(agentctl run --model claude-sonnet-4-6 --task-file /tmp/task.txt --cwd /repos/myapp 2>/dev/null)

# Block until done
agentctl run --model claude-sonnet-4-6 --task "fix the failing tests" --cwd /repos/myapp --wait
```

Exactly one of `--task` or `--task-file` must be provided.

The session ID is printed to **stdout**; hints go to **stderr** so `id=$(agentctl run ...)` works cleanly.

### Completion notifications

`agentctl run` can notify another system when the agent finishes.

#### Pi session follow-up

```bash
# Uses $PI_SESSION_ID automatically when present, but only when no explicit notifier is selected
id=$(agentctl run --model claude-sonnet-4-6 --task "..." 2>/dev/null)

# Or target a specific pi session explicitly
id=$(agentctl run --model claude-sonnet-4-6 --task "..." --notify-session "$PI_SESSION_ID" 2>/dev/null)
```

#### Munin shorthand

When Munin provides these environment variables:
- `MUNIN_EVENTS_DIR`
- `MUNIN_CHANNEL_ID`
- `MUNIN_THREAD_TS` (optional)

use the shorthand flag:

```bash
id=$(agentctl run --model openai/gpt-5.4 --task "..." --notify-munin 2>/dev/null)
```

#### Write an immediate event file explicitly

Useful outside Munin or when you want to override the defaults:

```bash
id=$(agentctl run \
  --model openai/gpt-5.4 \
  --task "..." \
  --notify-event-dir /workspace/events \
  --notify-event-channel C123 \
  --notify-event-thread 1710000000.000100 \
  2>/dev/null)
```

This writes an `immediate` event JSON file when the agent completes.

### Provider syntax

Use `provider/model` when a model name is ambiguous across providers:

```bash
agentctl run --model openai/gpt-5.4 --task "..."
```

### Monitor live output

```bash
agentctl monitor              # all running sessions
agentctl monitor <id> <id>   # specific sessions
```

Labels default to the model name. The short ID is only appended when two sessions share the same model:

```
[claude-sonnet-4-6]  Nodes whisper across the wire,
[gpt-5.4]            Consensus blooms where failures test the light.
```

Use `--name` for readable labels:

```bash
id1=$(agentctl run --model claude-sonnet-4-6 --name sonnet --task "..." 2>/dev/null)
id2=$(agentctl run --model openai/gpt-5.4   --name gpt    --task "..." 2>/dev/null)
agentctl monitor $id1 $id2
# [sonnet] ...
# [gpt]    ...
```

### Read output

```bash
agentctl dump <id>          # last 100 lines (rendered from JSON)
agentctl dump <id> -n 200   # last N lines
agentctl dump <id> --json   # raw NDJSON events
agentctl dump <id> --summary # condensed (tool calls + final text only)
agentctl dump <id> -f       # follow mode (like tail -f, rendered)
agentctl dump <id> -f --json  # follow mode with raw NDJSON
```

Parses the JSON event log and renders human-readable output including assistant text, tool calls with arguments, tool results, token counts, costs, and turn boundaries. Works both while the agent is running and after completion. Follow mode (`-f`) streams output in real-time and stops when the session ends.

### Quick status

```bash
agentctl status <id>    # one-line summary: "thinking", "running bash: echo hello", etc.
```

### Manual intervention

```bash
agentctl attach <id>   # attach terminal to the tmux session; detach with Ctrl+b d
```

Use this when an agent is waiting for confirmation or needs auth.

### List & clean up

```bash
agentctl ls                 # list sessions with status, cost, and activity
agentctl ls --since 1d      # only sessions from the last day
agentctl ls --model opus    # filter by model name
agentctl costs              # total API costs across all sessions
agentctl costs --since 1d   # costs from the last day only
agentctl kill <id>          # kill session, preserve log
agentctl kill --all         # kill all sessions
```

## How it works

Each `agentctl run` creates a tmux session that runs:

```sh
pi --mode json --model <model> --no-session -p "<task>" 2><logfile>.stderr | agentctl record <logfile>
```

Pi runs in JSON mode, producing streaming NDJSON events (text deltas, tool calls, tool results) on stdout. Stderr is redirected to a separate `.stderr` file to keep the NDJSON log clean (pi emits terminal escape sequences on stderr that can be very large). `agentctl record` mirrors the raw JSON stream to the terminal while stripping large `partial`/`message` payloads from `thinking_delta` and `text_delta` events before persisting them to the log file. Non-JSON lines are filtered out. This keeps recordings linear in size and still enables real-time progress monitoring via `dump` and `monitor`.

When pi exits the tmux session is destroyed automatically, flipping the session status to `done`.

When `--notify-session`, `--notify-munin`, or `--notify-event-dir` is set, `agentctl` also spawns a detached watcher process that waits for the tmux session to disappear and then sends the configured completion notification(s).

Session metadata is stored in `~/.local/share/agentctl/sessions/`. Log files live in `~/.local/share/agentctl/logs/` and survive `kill`.

## Commands

| Command | Description |
|---|---|
| `run --model <m> (--task <t>\|--task-file <path>)` | Spawn a pi agent session |
| `ls` | List sessions with status, age, and cost |
| `status <id>` | One-line summary (thinking, running bash, writing...) |
| `monitor [id...]` | Stream live labeled output |
| `dump <id> [-n] [-f] [--json]` | Print/follow rendered output (or raw JSON) |
| `attach <id>` | Attach terminal for manual intervention |
| `costs` | Show per-session and total API costs |
| `kill <id> / --all` | Kill session(s), preserve logs |
