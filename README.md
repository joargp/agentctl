# agentctl

CLI for running and monitoring [pi](https://buildwithpi.ai) coding agent sessions. Spawn agents with any model, stream their output live, and watch multiple sessions side by side.

## Install

```bash
go install github.com/joargp/agentctl@latest
```

Requires `tmux` and `pi` in `$PATH`.

## Quick start

```bash
# Spawn an agent — the session ID is printed to stdout, hints to stderr
id=$(agentctl run --model claude-opus-4-6 --task "add unit tests to the auth module" --cwd /repos/myapp 2>/dev/null)

agentctl status $id     # one-line summary: "thinking", "running bash: echo hello", ...
agentctl dump $id -f    # follow rendered output (like tail -f)
agentctl monitor        # stream live output from all running sessions
agentctl ls             # list sessions with status, cost, and activity
agentctl kill $id       # kill the session, preserve its log
```

## Commands

| Command | Description |
|---|---|
| `run --model <m> (--task <t>\|--task-file <path>)` | Spawn a pi agent session |
| `ls` | List sessions with status, age, and cost |
| `status <id>` | One-line summary (thinking, running bash, writing...) |
| `monitor [id...]` | Stream live labeled output |
| `dump <id> [-n] [-f] [--json] [--summary]` | Print/follow rendered output (or raw JSON) |
| `attach <id>` | Attach terminal for manual intervention |
| `costs` | Show per-session and total API costs |
| `kill <id> / --all` | Kill session(s), preserve logs |

## Running agents

```bash
# Pass the task from a file (safer for large prompts)
id=$(agentctl run --model claude-opus-4-6 --task-file /tmp/task.txt 2>/dev/null)

# Block until done
agentctl run --model claude-opus-4-6 --task "fix the failing tests" --wait

# Set the thinking level (off, minimal, low, medium, high, xhigh)
id=$(agentctl run --model claude-opus-4-6 --thinking high --task "refactor the session store" 2>/dev/null)

# Use provider/model when a model name is ambiguous across providers
agentctl run --model openai/gpt-5.4 --task "..."

# Name sessions for readable monitor labels (default label is the model name)
id=$(agentctl run --model claude-opus-4-6 --name opus --task "..." 2>/dev/null)
```

Exactly one of `--task` or `--task-file` must be provided.

## Reading output

```bash
agentctl dump <id>           # last 100 lines (rendered from JSON)
agentctl dump <id> -n 200    # last N lines
agentctl dump <id> --json    # raw NDJSON events
agentctl dump <id> --summary # condensed (tool calls + final text only)
agentctl dump <id> -f        # follow mode, stops when the session ends
```

`dump` renders assistant text, tool calls with arguments, tool results, token counts, costs, and turn boundaries — both while the agent is running and after completion.

Use `agentctl attach <id>` when an agent is waiting for confirmation or needs auth (detach with `Ctrl+b d`).

## Completion notifications

`agentctl run` can notify another system when the agent finishes:

- `--notify-session <id>` — follow up in a pi session (defaults to `$PI_SESSION_ID` when no notifier is selected)
- `--notify-munin` — shorthand for Munin's event-file convention
- `--notify-event-dir/-channel/-thread` — write an event JSON file explicitly
- `--notify-command <path>` — run an executable with a completion JSON payload on stdin

See [docs/notifications.md](docs/notifications.md) for the payload schema, the bundled Codex notifier, and configuration details.

## How it works

Each `agentctl run` creates a tmux session running a small `agentctl supervise` wrapper around:

```sh
pi --mode json --model <model> --no-session -p "<task>" 2><logfile>.stderr | agentctl record <logfile>
```

The supervisor streams pi's NDJSON output to the terminal while writing a sanitized log (large `thinking_delta`/`text_delta` payloads are stripped, keeping recordings linear in size). It runs pi in a dedicated process group and tears down the full process tree on exit, crash, timeout, or `agentctl kill`. When pi exits, the tmux session is destroyed and the session status flips to `done`.

State lives under `~/.local/share/agentctl/`: session metadata in `sessions/`, logs in `logs/` (these survive `kill`), and runtime PID/PGID state in `runtime/` while a session is active.
