# Plan 002: Add a safe `agentctl prune` command that reclaims log disk without deleting session history

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 824a6b2..HEAD -- cmd/kill.go cmd/ls.go cmd/session_stats.go cmd/watch.go cmd/root.go cmd/completion.go internal/session/session.go README.md plans/README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `824a6b2`, 2026-07-12

## Why this matters

Local agentctl state grows quickly because completed sessions keep full NDJSON logs forever. On the author's machine this is already multi-GB under `~/.local/share/agentctl/logs` while session JSON alone is only tens of MB. Users still want old sessions as a learning corpus (task text, model, turns, cost). v1 prune should reclaim disk by removing **bulky log artifacts for old finished sessions**, while **keeping session metadata**. No hard-delete of history, no automatic GC, no retention tiers yet.

## Current state

Relevant files and roles:

- `cmd/root.go` — registers data dirs (`sessions`, `logs`, `scripts`, `runtime`) and the root Cobra command.
- `cmd/kill.go` — closest lifecycle command; kills running work and optionally removes logs with `--clean`, but also removes session metadata.
- `cmd/ls.go` — has shared `parseDuration` (supports `2d`) and listing filters; good pattern for flag naming and duration parsing.
- `cmd/session_stats.go` — `cacheSessionLogStats` / `getSessionLogStats` already persist turns/cost into session JSON so logs are not required for cost history.
- `cmd/watch.go` — `extractLastTurnText` already knows how to pull the final assistant text from a log (reuse later if you add summary caching; not required for v1 if it complicates things).
- `internal/session/session.go` — `Session` struct, `List`/`Load`/`Save`/`Remove`/`DataDir`.
- `internal/tmux/tmux.go` — `SessionExists` / `ListSessions` for running checks.
- `README.md` — command table and usage docs.
- `cmd/ls_test.go` / `cmd/cache_test.go` — test patterns using `t.Setenv("HOME", t.TempDir())`.

Session metadata already carries the learnable fields (`task`, `model`, `thinking`, `cwd`, `name`, `turns`, `total_cost`, `started_at`). Example shape:

```json
{
  "id": "1d5ce2b1",
  "name": "tool-timeline-macos",
  "model": "accounts/fireworks/models/glm-5p2",
  "thinking": "xhigh",
  "task": "...",
  "cwd": "/Users/joar/repos/sklls/app/munin",
  "started_at": "2026-07-12T09:44:47.17243+02:00",
  "turns": 187,
  "total_cost": 4.37845326,
  "stats_cached": true
}
```

`kill` currently deletes metadata and optionally logs:

```go
// cmd/kill.go:68-80
// Remove script/task files. Log is preserved unless --clean is set.
filesToRemove := []string{s.ScriptFile, s.TaskFile, s.RuntimeFile}
if killClean {
	filesToRemove = append(filesToRemove, s.LogFile, s.LogFile+".stderr")
}
// ...
if err := session.Remove(id); err != nil {
	fmt.Fprintf(os.Stderr, "warn: remove session metadata: %v\n", err)
}
```

`parseDuration` already understands day units:

```go
// cmd/ls.go:47-56
func parseDuration(s string) (time.Duration, error) {
	// Support "d" suffix for days in addition to Go's standard durations.
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := strconv.ParseFloat(s[:len(s)-1], 64)
```

Conventions to match:

- One command per file under `cmd/`, registered in `init()` via `rootCmd.AddCommand`.
- Stdout for primary machine-readable/user output; stderr for warnings.
- Imperative single-line commit messages without conventional-commit prefixes (e.g. `Add prune command for reclaiming session log disk`).
- Tests isolate storage with `t.Setenv("HOME", t.TempDir())` — see `cmd/ls_test.go:154-156` and `cmd/cache_test.go:112-113`.
- Running = tmux session still exists (`tmux.SessionExists(s.TmuxSession)` or bulk `tmux.ListSessions()`).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Drift check | `git diff --stat 824a6b2..HEAD -- cmd/kill.go cmd/ls.go cmd/session_stats.go cmd/watch.go cmd/root.go cmd/completion.go internal/session/session.go README.md plans/README.md` | empty, or only expected local plan edits |
| Unit tests | `go test ./cmd/ -count=1` | PASS |
| Focused prune tests | `go test ./cmd/ -count=1 -run 'TestPrune\|TestParseDuration'` | PASS |
| Full tests | `go test ./... -count=1` | PASS |
| Build | `go build -o /tmp/agentctl .` | exit 0 |
| Help smoke | `/tmp/agentctl prune --help` | shows flags, mentions dry-run / older-than |
| Dry-run smoke | `HOME=$(mktemp -d) /tmp/agentctl prune --older-than 30d --dry-run` | exits 0; prints zero candidates / no sessions style summary |

## Scope

**In scope** (the only files you should create or modify):

- `cmd/prune.go` (create)
- `cmd/prune_test.go` (create)
- `README.md` (document the command in the Commands table + a short usage example)
- `plans/README.md` (status row only)

**Out of scope** (do NOT touch, even if related):

- Hard-deleting session JSON / full history wipe commands
- Pinning, named-session protection, keep-last-N tiers, size-based policies
- Compacting logs into smaller NDJSON (summary files, final_text fields) — deferred
- Dashboard API / UI
- Changing `kill` semantics
- Auto-prune on `run` or background GC
- Changing default log retention for new sessions
- `internal/session` schema changes (v1 must work with the existing `Session` struct)

## Git workflow

- Branch: `advisor/002-add-prune-command` (or work directly if the operator already has a branch; do not create extra branches if already on a feature branch they chose)
- Commit style from recent history: imperative, no `feat:` prefix, no trailing period
  - Example: `Add prune command for reclaiming session log disk`
- Do NOT push or open a PR unless the operator asked

## Product rules for v1 (non-negotiable)

1. **Keep session metadata.** Never call `session.Remove` from prune.
2. **Only prune finished sessions.** If the tmux session still exists, skip.
3. **Age gate is required.** `--older-than` is required (reuse `parseDuration` from `cmd/ls.go`). Reject missing/invalid values.
4. **Default action deletes bulky artifacts only:**
   - `<log>.log`
   - `<log>.log.stderr`
   - `<log>.log.watch.log` if present (watcher logs are named `<logfile>.watch.log` in docs; also check `s.LogFile + ".watch.log"`)
   - leftover `ScriptFile` / `TaskFile` / `RuntimeFile` / `CancelFile` if present
5. **Before deleting a log, ensure stats are cached** via existing `cacheSessionLogStats(s)` (or equivalent) so `ls`/`costs` keep turns/cost after the log is gone.
6. **`--dry-run` prints the plan and deletes nothing.**
7. **Idempotent.** Re-running prune on already-pruned sessions is a no-op success (skip if no reclaimable files remain).
8. **Never touch running sessions**, even if old.
9. **No interactive prompts.** This is a CLI for agents/scripts; safety comes from dry-run + clear summary, not `y/N`.

## Steps

### Step 1: Drift check and branch readiness

Run the drift check from the executor banner. Read the live versions of:

- `cmd/kill.go`
- `cmd/ls.go` (`parseDuration`, flag style)
- `cmd/session_stats.go`
- `internal/session/session.go`
- `README.md` Commands table

**Verify**: drift command exits 0; if in-scope source files differ from the excerpts above in a way that changes kill/list/stats behavior, STOP.

### Step 2: Add `cmd/prune.go`

Create `cmd/prune.go` modeled on other simple commands (`costs.go`, `kill.go`).

Suggested CLI surface:

```text
agentctl prune --older-than 30d --dry-run
agentctl prune --older-than 30d
agentctl prune --older-than 14d --cwd /repos/myapp
```

Flags:

| Flag | Required | Meaning |
|------|----------|---------|
| `--older-than` | yes | only sessions with `StartedAt` older than this duration (`1h`, `30m`, `2d`, etc. via `parseDuration`) |
| `--dry-run` | no | report candidates and reclaimable bytes; delete nothing |
| `--cwd` | no | optional substring filter on `s.Cwd` (same idea as `ls --cwd`) |
| `--model` | no | optional substring filter on normalized model (same idea as `ls --model`) |

Implementation sketch:

```go
var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Reclaim disk by removing old finished session logs",
	Long: `Delete bulky log artifacts for finished sessions older than a cutoff,
while keeping session metadata (task, model, turns, cost) so history remains
queryable via ls/costs/status.

Running sessions are never touched. Use --dry-run to preview.

Examples:
  agentctl prune --older-than 30d --dry-run
  agentctl prune --older-than 30d
  agentctl prune --older-than 14d --cwd /repos/myapp`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}
```

Selection logic for each session from `session.List()`:

1. If `tmux.SessionExists(s.TmuxSession)` (prefer one bulk `tmux.ListSessions()` snapshot like `ls` does) → skip as running.
2. If `time.Since(s.StartedAt) < olderThan` → skip as too new.
3. If `--cwd` / `--model` filters do not match → skip.
4. Build the candidate file list that currently exists among:
   - `s.LogFile`
   - `s.LogFile + ".stderr"`
   - `s.LogFile + ".watch.log"`
   - `s.ScriptFile`
   - `s.TaskFile`
   - `s.RuntimeFile`
   - `s.CancelFile`
5. If none exist → skip (already pruned / nothing to reclaim).
6. Else candidate for prune.

Apply path (not dry-run):

1. If `s.LogFile` exists and stats are not already trustworthy (`!(s.StatsCached || s.Turns > 0)`), call `cacheSessionLogStats(s)` first. If caching fails, **skip that session** and warn on stderr — do not delete the log without a best-effort stats cache attempt.
2. Remove each existing reclaimable file with `os.Remove`. Collect/remove errors per session; continue with others.
3. Do **not** remove the session JSON.

Output (stdout), human-readable and stable enough for eyeballing:

```text
PRUNE 30d  dry-run=true
candidates: 12
running skipped: 2
too new skipped: 40
already clean: 5
reclaimable: 1.8G

abc12345  45d  120.4M  model=gpt-5.4  task=fix the auth tests...
def67890  32d  18.1M   model=claude-opus-4-6  task=review PR...

# when not dry-run, end with:
pruned 12 sessions; reclaimed 1.8G
```

Use simple byte formatting (`120.4M`, `1.8G`) — match style used elsewhere if present, otherwise implement a tiny local helper in `prune.go`.

Register the command in `init()` with `rootCmd.AddCommand(pruneCmd)`.

**Verify**:
- `go test ./cmd/ -count=1 -run TestParseDuration` still passes
- `go build -o /tmp/agentctl .` succeeds
- `/tmp/agentctl prune --help` shows the new command and flags
- `/tmp/agentctl prune` without `--older-than` exits non-zero with a clear error

### Step 3: Tests in `cmd/prune_test.go`

Follow the HOME isolation pattern from `cmd/ls_test.go` / `cmd/cache_test.go`.

Create fixtures under `filepath.Join(home, ".local", "share", "agentctl", ...)` by saving sessions with `session.Save` and writing fake log/script files.

Required cases:

1. **`TestPruneDryRunDoesNotDelete`**
   - Old finished session (StartedAt 40d ago), log + stderr exist, no need for live tmux (mock or ensure tmux lookup sees it as not running).
   - Because tests may not have tmux, mirror `ls` tests: if production code calls `tmux.ListSessions` / `SessionExists`, introduce a package-level seam like `ls` did with `listTmuxSessionsForLs` **only if needed**. Prefer:

   ```go
   var pruneSessionRunning = func(s *session.Session) bool {
       return tmux.SessionExists(s.TmuxSession)
   }
   ```

   and override in tests to return false/true.
   - Run dry-run → files still exist; output mentions the session id and reclaimable size.

2. **`TestPruneDeletesLogsKeepsSessionJSON`**
   - Same fixture; run without dry-run.
   - Log/stderr/script/task removed.
   - `session.Load(id)` still works and retains `Task` / `Model` / cached turns+cost.

3. **`TestPruneSkipsRunningSessions`**
   - `pruneSessionRunning` returns true → files untouched.

4. **`TestPruneSkipsRecentSessions`**
   - StartedAt 1h ago with `--older-than 30d` → untouched.

5. **`TestPruneIdempotent`**
   - Prune twice → second run reports 0 candidates / already clean; exit 0.

6. **`TestPruneCachesStatsBeforeDeletingLog`**
   - Session with `Turns=0`, `StatsCached=false`, log containing one `turn_end` with cost.
   - After prune: session JSON has turns/cost populated and log gone.
   - You can reuse the log snippet style from `cmd/ls_test.go` cost/turn tests.

7. **`TestPruneRequiresOlderThan`**
   - Invoke the command path / validation with empty older-than → error.

Keep tests deterministic: do not depend on the developer’s real `~/.local/share/agentctl`.

**Verify**: `go test ./cmd/ -count=1 -run TestPrune` → all new tests PASS.

### Step 4: README docs

Update `README.md`:

1. Add a row to the Commands table:

   `| `prune --older-than <dur>` | Remove old finished session logs; keep metadata |`

2. Add a short subsection near kill/cleanup docs (or under a small “Maintenance” / after “Commands” examples):

```bash
# Preview what would be reclaimed
agentctl prune --older-than 30d --dry-run

# Delete old finished session logs (session history in ls/costs is kept)
agentctl prune --older-than 30d
```

State explicitly: running sessions are skipped; session JSON is kept; this is not a full history wipe.

**Verify**: README contains `prune` and `--dry-run`; no claim that metadata is deleted.

### Step 5: Full verification + plan index

Run:

```bash
go test ./... -count=1
go build -o /tmp/agentctl .
/tmp/agentctl prune --help
```

Update `plans/README.md` status for plan 002 to `DONE` (or leave `TODO` only if you were told not to touch the index; default: update when complete).

**Verify**: full test suite green; binary help works; `git status` shows only in-scope files.

## Test plan

- New file: `cmd/prune_test.go`
- Pattern: `cmd/ls_test.go` + `cmd/cache_test.go` (temp HOME, session.Save fixtures)
- Cases listed in Step 3
- Verification: `go test ./cmd/ -count=1 -run TestPrune` and `go test ./... -count=1`

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `go test ./... -count=1` exits 0
- [ ] `go build -o /tmp/agentctl .` exits 0
- [ ] `/tmp/agentctl prune --help` documents `--older-than` and `--dry-run`
- [ ] `/tmp/agentctl prune` (no flags) fails with a clear `--older-than` error
- [ ] New tests cover: dry-run non-deletion, log deletion with metadata kept, running skip, recent skip, idempotency, stats cache-before-delete
- [ ] Prune never calls `session.Remove`
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row for 002 updated

## STOP conditions

Stop and report back (do not improvise) if:

- Drift check shows kill/list/stats/session APIs changed such that the excerpts are wrong.
- Implementing running-session detection cleanly seems to require rewriting tmux internals.
- You believe v1 must delete session JSON to be useful (it must not; report instead).
- Caching stats before log deletion cannot reuse `cacheSessionLogStats` without large refactors.
- A verification command fails twice after a reasonable fix attempt.
- Scope pressure appears to pull in pin/keep-last-N/dashboard/final_text compaction — leave those for a follow-up plan.

## Maintenance notes

- Reviewers should confirm prune cannot delete running-session logs and cannot remove session JSON.
- Follow-up ideas intentionally deferred:
  - keep-last-N full logs
  - pin / named-session protection
  - store `final_text` summary on the session before log deletion
  - hard-delete mode for metadata
  - size-based policies / automatic GC
- Once prune exists, `List()` may still recover log-only sessions for IDs that still have logs; after prune, metadata-only sessions remain listable via session JSON, which is desired.
- If a future “compact log” mode lands, it should be a separate flag/command rather than changing v1 defaults.
