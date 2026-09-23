# Plan 005: Codex notifier success-path tests no longer time out under parallel load

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 1564c7c..HEAD -- cmd/agentctl-notify-codex/main_test.go`
> If it changed, compare the excerpts below against the live code; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `1564c7c`, 2026-09-23

## Why this matters

`TestRunSendsMessageToCodexAppServer` normally finishes in ~0.25s but gives the whole exchange with a forked `/bin/sh` fake server only a **2-second** timeout. During `go test -cover ./...` (all packages compiled and run in parallel) it failed with `initialize request 1: context deadline exceeded` — observed on 2026-09-23 at commit `1564c7c`, then passed on 5 reruns. Flaky tests train people to ignore red builds. The fix: give tests that expect success a generous timeout; keep the short timeout only where the test *expects* a timeout.

## Current state

File: `cmd/agentctl-notify-codex/main_test.go`. Three tests pass a timeout via the injected `getenv`:

| Test | Line (approx) | Expects | Current timeout |
|---|---|---|---|
| `TestRunSendsMessageToCodexAppServer` | 47-48 | success | `"2"` |
| `TestRunReturnsCodexTurnFailure` | 111-112 | fast error from fake server (`boom`) | `"2"` |
| `TestRunWaitsForTurnCompletion` | 141-142 | **timeout** (`wait for turn/completed`) | `"2"` |

Pattern at each site:
```go
			case "AGENTCTL_CODEX_TIMEOUT_SECONDS":
				return "2"
```
The timeout is applied in `cmd/agentctl-notify-codex/main.go` `run()` via `context.WithTimeout(parent, timeoutFromEnv(...))`; the value is whole seconds.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Package tests | `go test ./cmd/agentctl-notify-codex/ -count=1` | `ok` |
| Stress | `go test ./cmd/agentctl-notify-codex/ -count=20 -cpu 1,4` | `ok` |
| Full suite under load | `go test -cover -count=1 ./...` | all `ok` |

## Scope

**In scope**: `cmd/agentctl-notify-codex/main_test.go` only.

**Out of scope**: `cmd/agentctl-notify-codex/main.go` (production timeout `defaultTimeout = 90s` is fine); `TestRunWaitsForTurnCompletion`'s `"2"` — it must stay short because the test deliberately waits for the timeout to fire.

## Git workflow

- Current branch; single-line imperative commit, no prefix, no period (e.g. `Raise timeouts in Codex notifier success tests`). Do NOT push.

## Steps

### Step 1: Raise the success-path timeouts

In `TestRunSendsMessageToCodexAppServer` and `TestRunReturnsCodexTurnFailure`, change `return "2"` under `AGENTCTL_CODEX_TIMEOUT_SECONDS` to `return "30"`. Do not change `TestRunWaitsForTurnCompletion`.

Both tests finish as soon as the fake server replies, so a larger ceiling adds no wall time in the normal case.

**Verify**: `grep -n 'return "2"' cmd/agentctl-notify-codex/main_test.go` → exactly 1 match (inside `TestRunWaitsForTurnCompletion`); `grep -c 'return "30"' cmd/agentctl-notify-codex/main_test.go` → `2`.

### Step 2: Stress

**Verify**: `go test ./cmd/agentctl-notify-codex/ -count=20 -cpu 1,4` → `ok`; then `go test -cover -count=1 ./...` → all `ok`.

## Test plan

No new tests; this plan makes existing tests deterministic. The stress command in Step 2 is the check.

## Done criteria

- [ ] `go test ./cmd/agentctl-notify-codex/ -count=20 -cpu 1,4` → `ok`
- [ ] `go test -cover -count=1 ./...` → all `ok`
- [ ] Only `cmd/agentctl-notify-codex/main_test.go` and `plans/README.md` modified (`git status`)

## STOP conditions

- The stress run still fails with `context deadline exceeded` at 30s — then the hang is real (e.g. a pipe/read deadlock in the RPC client), not load; report with the failing output.
- `TestRunWaitsForTurnCompletion` no longer uses a short timeout or has been restructured.

## Maintenance notes

- New tests in this package: use a generous timeout when success is expected; use a short timeout only when the test asserts on the timeout itself.
