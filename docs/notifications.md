# Completion notifications

`agentctl run` can notify another system when the agent finishes. When any notify flag is set, `agentctl` spawns a detached watcher process that waits for the tmux session to disappear and then sends the configured notification(s). Watcher stdout/stderr is written to `<logfile>.watch.log`, e.g. `~/.local/share/agentctl/logs/abc12345.watch.log`.

## Pi session follow-up (`--notify-session`)

```bash
# Uses $PI_SESSION_ID automatically when present, but only when no explicit notifier is selected
id=$(agentctl run --model claude-opus-4-6 --task "..." 2>/dev/null)

# Or target a specific pi session explicitly
id=$(agentctl run --model claude-opus-4-6 --task "..." --notify-session "$PI_SESSION_ID" 2>/dev/null)
```

## Munin shorthand (`--notify-munin`)

When Munin provides `MUNIN_EVENTS_DIR`, `MUNIN_CHANNEL_ID`, and optionally `MUNIN_THREAD_TS`:

```bash
id=$(agentctl run --model openai/gpt-5.4 --task "..." --notify-munin 2>/dev/null)
```

## Event file (`--notify-event-*`)

Writes an `immediate` event JSON file on completion. Useful outside Munin or to override its defaults:

```bash
id=$(agentctl run \
  --model openai/gpt-5.4 \
  --task "..." \
  --notify-event-dir /workspace/events \
  --notify-event-channel C123 \
  --notify-event-thread 1710000000.000100 \
  2>/dev/null)
```

## Executable notifier (`--notify-command`)

Invokes an executable with completion JSON on stdin:

```bash
id=$(agentctl run \
  --model openai/gpt-5.4 \
  --task "..." \
  --notify-command ./scripts/agentctl-notify-codex \
  2>/dev/null)
```

The command value must be one explicit executable path, such as `./notify` or `/usr/local/bin/notify`. Bare command names and command strings with arguments are rejected; `agentctl` does not look up notifier commands from `$PATH` and does not run them through a shell. Executable notifiers have a 120 second timeout.

Notifier commands inherit the watcher environment and receive this payload:

```json
{
  "schemaVersion": 1,
  "event": "session.completed",
  "session": {
    "id": "abc12345",
    "name": "optional-name",
    "model": "claude-opus-4-6",
    "task": "original task",
    "cwd": "/repo/path",
    "startedAt": "2026-06-08T12:00:00Z",
    "logFile": "/Users/me/.local/share/agentctl/logs/abc12345.log",
    "turns": 3,
    "totalCost": 0.03
  },
  "message": "Agent **claude-opus-4-6** (`abc12345`) finished...",
  "dumpCommand": "agentctl dump abc12345"
}
```

`agentctl` does not depend on Codex. Codex, Slack, or other integrations should live in notifier executables that consume this payload.

## Codex notifier

This repo includes an external Codex notifier binary. Install it separately:

```bash
go install github.com/joargp/agentctl/cmd/agentctl-notify-codex@latest
```

Then invoke it by explicit path. From inside a Codex thread, pass the current thread ID through `AGENTCTL_CODEX_THREAD_ID` so the detached watcher has an explicit target:

```bash
id=$(AGENTCTL_CODEX_THREAD_ID="$CODEX_THREAD_ID" \
  agentctl run \
  --model claude-opus-4-6 \
  --task "..." \
  --notify-command "$(command -v agentctl-notify-codex)" \
  2>/dev/null)
```

The notifier starts `codex app-server`, resumes the target thread, sends the completion message with `turn/start`, and exits after Codex completes the turn. Configuration is environment-only because `--notify-command` accepts a single executable path:

- `AGENTCTL_CODEX_THREAD_ID` overrides the target thread.
- `CODEX_THREAD_ID` is used when `AGENTCTL_CODEX_THREAD_ID` is unset.
- `AGENTCTL_CODEX_BIN` overrides the `codex` binary path.
- `AGENTCTL_CODEX_TIMEOUT_SECONDS` overrides the notifier's internal timeout (default: 90 seconds). Keep it below `agentctl`'s command notifier timeout unless you invoke `agentctl-notify-codex` directly.

To target a specific Codex thread explicitly:

```bash
AGENTCTL_CODEX_THREAD_ID=019ea641-f54e-7c20-ab26-0edfcd41445b \
  agentctl run \
    --model claude-opus-4-6 \
    --task "..." \
    --notify-command "$(command -v agentctl-notify-codex)"
```
