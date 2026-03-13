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
id=$(agentctl run --model claude-opus-4-6 --task "add unit tests to the auth module" --cwd /repos/myapp 2>/dev/null)

# Block until done
agentctl run --model claude-opus-4-6 --task "fix the failing tests" --cwd /repos/myapp --wait
```

The session ID is printed to **stdout**; hints go to **stderr** so `id=$(agentctl run ...)` works cleanly.

### Provider syntax

Use `provider/model` when a model name is ambiguous across providers:

```bash
agentctl run --model openai-codex/gpt-5.3-codex --task "..."
```

### Monitor live output

```bash
agentctl monitor              # all running sessions
agentctl monitor <id> <id>   # specific sessions
```

Labels default to the model name. The short ID is only appended when two sessions share the same model:

```
[claude-opus-4-6]    Nodes whisper across the wire,
[gpt-5.3-codex]      Consensus blooms where failures test the light.
```

Use `--name` for readable labels:

```bash
id1=$(agentctl run --model claude-opus-4-6            --name opus  --task "..." 2>/dev/null)
id2=$(agentctl run --model openai-codex/gpt-5.3-codex --name codex --task "..." 2>/dev/null)
agentctl monitor $id1 $id2
# [opus]   ...
# [codex]  ...
```

### Read output

```bash
agentctl dump <id>          # last 100 lines
agentctl dump <id> -n 200   # last N lines
```

Reads from the live pane while running, log file after completion. Useful for feeding agent output back to another LLM.

### Manual intervention

```bash
agentctl attach <id>   # attach terminal to the tmux session; detach with Ctrl+b d
```

Use this when an agent is waiting for confirmation or needs auth.

### List & clean up

```bash
agentctl ls             # list sessions with status (running / done)
agentctl kill <id>      # kill session, preserve log
agentctl kill --all     # kill all sessions
```

## How it works

Each `agentctl run` creates a tmux session that runs:

```sh
exec pi --model <model> --no-session -p "<task>"
```

Output is streamed to a log file via `tmux pipe-pane`. When pi exits the tmux session is destroyed automatically, flipping the session status to `done`.

Session metadata is stored in `~/.local/share/agentctl/sessions/`. Log files live in `~/.local/share/agentctl/logs/` and survive `kill`.

## Commands

| Command | Description |
|---|---|
| `run --model <m> --task <t>` | Spawn a pi agent session |
| `ls` | List sessions with status and age |
| `monitor [id...]` | Stream live labeled output |
| `dump <id> [-n lines]` | Print last N lines of output |
| `attach <id>` | Attach terminal for manual intervention |
| `kill <id> / --all` | Kill session(s), preserve logs |
