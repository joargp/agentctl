# Plan 004: Dashboard log stream sends each line of a running session exactly once

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 1564c7c..HEAD -- internal/dashboard/server.go internal/dashboard/server_test.go`
> Plan 003 is expected to have changed these files (loopback bind, id validation at the top of `handleSessionLogs`, a kill hook). That is fine. Any *other* change to `handleSessionLogs` beyond the id check is a mismatch → STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: plans/003-harden-dashboard-server.md (same file; run after it to avoid conflicts)
- **Category**: bug
- **Planned at**: commit `1564c7c`, 2026-09-23

## Why this matters

When you open a **running** session in `agentctl dashboard`, the entire existing log is rendered twice: the handler first streams the whole file, then starts `tail.TailFile` with no `Location`, and `nxadm/tail` defaults to reading from the **start** of the file. Every turn, thinking block, and text delta before the moment you clicked is duplicated in the UI. Completed sessions are unaffected (the handler returns before tailing). After this plan, the tailer resumes at exactly the byte offset where the initial read stopped.

## Current state

File: `internal/dashboard/server.go`, function `handleSessionLogs` (starts ~line 691). Relevant part (`server.go:710-742` at planning time):
```go
	// First stream existing logs
	file, err := os.Open(s.LogFile)
	if err == nil {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 128*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
		}
		file.Close()
	}

	// Send caught_up event
	fmt.Fprintf(w, "event: caught_up\ndata: {}\n\n")
	flusher.Flush()

	running := tmux.SessionExists(s.TmuxSession)
	if !running {
		fmt.Fprintf(w, "event: end\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	// Start tailing
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow: true,
		ReOpen: true,
		Poll:   true,
	})
```
The rest of the function is a `select` loop over `r.Context().Done()`, a `time.NewTicker(5 * time.Second)` ping that also re-checks `tmux.SessionExists` (and drains remaining lines then sends `event: end`), and `t.Lines`.

Second subtlety: the log is being appended while we read it. `bufio.Scanner` returns a final line even without a trailing `\n`, so a half-written last line could be sent, and then sent again by the tailer once completed. The fix counts only **complete** (`\n`-terminated) lines toward the offset.

Existing precedent for seeking: `cmd/stream.go:67` uses `Location: &tail.SeekInfo{Offset: 0, Whence: 0}`.

Hook convention (`server.go:36-46`): package-level vars such as `listTmuxSessionsForDashboard = tmux.ListSessions`, saved/restored in `resetDashboardState` (`server_test.go:21-63`).

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Dashboard tests | `go test ./internal/dashboard/ -count=1` | `ok` |
| Race check | `go test ./internal/dashboard/ -count=1 -race -run LogStream` | `ok` |
| All tests | `go test ./... -count=1` | all `ok` |

## Scope

**In scope**: `internal/dashboard/server.go` (only `handleSessionLogs` + new hook vars), `internal/dashboard/server_test.go`.

**Out of scope**: `web/src/app.ts`, `internal/dashboard/static/*` (the client closes the EventSource on error and never auto-reconnects, so it needs no change); `cmd/stream.go`, `cmd/dump.go`; the SSE event names/format (`data:`, `caught_up`, `end`) — the frontend depends on them.

## Git workflow

- Current branch; single-line imperative commit, no prefix, no period (e.g. `Fix duplicated history in dashboard log stream`). Do NOT push.

## Steps

### Step 1: Add hooks

Next to the existing hooks in `server.go`:
```go
	// Kept as variables so log streaming can be tested without tmux or 5s waits.
	sessionExistsForDashboard = tmux.SessionExists
	dashboardLogPingInterval  = 5 * time.Second
```
Replace both `tmux.SessionExists(s.TmuxSession)` calls in `handleSessionLogs` with `sessionExistsForDashboard(s.TmuxSession)`, and `time.NewTicker(5 * time.Second)` with `time.NewTicker(dashboardLogPingInterval)`. Add both vars to the save/restore list in `resetDashboardState`.

**Verify**: `go build ./... && go test ./internal/dashboard/ -count=1` → `ok`

### Step 2: Write the failing test first

Add the tests in the Test plan below. **Verify**: `go test ./internal/dashboard/ -count=1 -run LogStream` → `TestHandleSessionLogsStreamsRunningLogOnce` **FAILS** (lines appear twice); the completed-session test passes. If the running test passes before the fix, STOP.

### Step 3: Track the byte offset of complete lines

Replace the "First stream existing logs" block with a reader that only consumes complete lines:
```go
	// Stream complete lines already in the log and remember where they end, so
	// the tailer resumes there instead of replaying the file from the start.
	var offset int64
	var partial string
	if file, err := os.Open(s.LogFile); err == nil {
		reader := bufio.NewReaderSize(file, 128*1024)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				partial = line // incomplete last line (possibly still being written)
				break
			}
			offset += int64(len(line))
			if text := strings.TrimRight(line, "\r\n"); strings.TrimSpace(text) != "" {
				fmt.Fprintf(w, "data: %s\n\n", text)
			}
		}
		file.Close()
		flusher.Flush()
	}
```
Then, in the not-running branch, emit the partial line before `end` (a finished log may lack a trailing newline):
```go
	if !sessionExistsForDashboard(s.TmuxSession) {
		if strings.TrimSpace(partial) != "" {
			fmt.Fprintf(w, "data: %s\n\n", partial)
		}
		fmt.Fprintf(w, "event: end\ndata: {}\n\n")
		flusher.Flush()
		return
	}
```
Keep the `caught_up` event between the initial read and this check, as today. Add `Location: &tail.SeekInfo{Offset: offset, Whence: io.SeekStart}` to the `tail.Config`. (`io` is already imported.) Leave the rest of the loop unchanged.

**Verify**: `go test ./internal/dashboard/ -count=1 -run LogStream` → both PASS; `go test ./internal/dashboard/ -count=1 -race -run LogStream` → `ok`

## Test plan

In `server_test.go`, modeled on `TestHandleSessionsUsesOneRunningSnapshotAndPreservesStatus` (uses `resetDashboardState`, `t.Setenv("HOME", t.TempDir())`, `saveDashboardSession`):

1. `TestHandleSessionLogsStreamsRunningLogOnce`:
   - Log file with two lines `{"type":"text_delta","delta":"one"}\n{"type":"text_delta","delta":"two"}\n`; save a session (id `running`) pointing `LogFile` at it.
   - `var running atomic.Bool; running.Store(true)`; `sessionExistsForDashboard = func(string) bool { return running.Load() }`; `dashboardLogPingInterval = 50 * time.Millisecond`.
   - `req := httptest.NewRequest(GET, "/api/sessions/running/logs", nil)`; `req.SetPathValue("id", "running")`; `rec := httptest.NewRecorder()`; run `handleSessionLogs(rec, req)` in a goroutine that closes a `done` channel.
   - Sleep 300ms, append `{"type":"text_delta","delta":"three"}\n` to the log, sleep 1500ms (tail polls every 250ms), then `running.Store(false)`.
   - Wait on `done` with a 10s timeout (`t.Fatal` on timeout). Only read `rec.Body` **after** `done` (avoids a data race).
   - Assert `strings.Count(body, "\"delta\":\"one\"") == 1`, same for `two` and `three`; assert body contains `event: caught_up` and `event: end`.
2. `TestHandleSessionLogsCompletedLogWithoutTrailingNewline`:
   - Log `{"a":1}\n{"a":2}` (no final newline); `sessionExistsForDashboard` returns false.
   - Call handler synchronously; assert `{"a":1}` and `{"a":2}` each appear exactly once and body ends with `event: end\ndata: {}\n\n`.

If Plan 003 has landed, the handler validates ids — `running` passes its pattern.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/dashboard/ -count=1 -race` → `ok`, including the 2 new tests
- [ ] `grep -n 'tail.SeekInfo{Offset: offset' internal/dashboard/server.go` → 1 match
- [ ] `grep -n 'tmux.SessionExists(s.TmuxSession)' internal/dashboard/server.go` → no matches
- [ ] `git status` shows only the two in-scope files and `plans/README.md` modified

## STOP conditions

- `handleSessionLogs` differs from the excerpt beyond Plan 003's id check.
- The running-session test passes **before** Step 3 (then the premise is wrong — report).
- The test is flaky across 5 runs (`go test ./internal/dashboard/ -count=5 -run LogStream`) after one attempt to lengthen the sleeps.
- `tail.SeekInfo` with `Poll: true` does not honor the offset in this `nxadm/tail` version (v1.4.11).

## Maintenance notes

- Any future change to the initial read must keep `offset` = bytes of **complete** lines only; the tailer relies on it.
- If log rotation or truncation is ever introduced (`prune` currently only deletes logs of finished sessions), revisit `ReOpen` + offset behavior.
- Deferred: lines longer than memory are unbounded with `ReadString`; the old scanner capped at 10 MB. Sanitized logs (`internal/session/recording.go`) keep lines small, so this is acceptable.
