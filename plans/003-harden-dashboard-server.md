# Plan 003: Dashboard only serves loopback, rejects cross-site/rebinding requests, and cannot be tricked into `kill --all`

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 1564c7c..HEAD -- internal/dashboard/server.go internal/dashboard/server_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `1564c7c`, 2026-09-23

## Why this matters

`agentctl dashboard` is an unauthenticated HTTP server that (a) listens on **every network interface**, so anyone on the same Wi-Fi/LAN can read every agent's task prompt and full log and kill agents; (b) accepts any `Host` header, so a malicious web page can use DNS rebinding to read those logs through the user's own browser; (c) accepts cross-site `POST`s, so any web page the user visits can `fetch("http://localhost:8080/api/sessions/<id>/kill", {method:"POST", mode:"no-cors"})`; and (d) passes the URL path segment straight to `agentctl kill <id>` as argv — an id of `--all` is parsed by cobra as the `--all` flag and **kills every session**. Combined: any website can kill all running agents with one request. After this plan the server binds 127.0.0.1 only, rejects non-loopback `Host` headers, rejects cross-origin POSTs, validates session ids, and passes `--` before the id.

## Current state

Files:
- `internal/dashboard/server.go` — the whole dashboard server (routes, handlers, listener).
- `internal/dashboard/server_test.go` — tests; handlers are called directly (`handleSessions(recorder, httptest.NewRequest(...))`), and `runDashboard` is tested with injected `opener`/`serve` funcs.
- `cmd/kill.go` — `agentctl kill [<id>] [--all] [--clean]`; `--all` kills everything (`cmd/kill.go:38-40`). Do not modify.

Listener, `internal/dashboard/server.go:346-360`:
```go
	mux.HandleFunc("GET /api/sessions", handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}/logs", handleSessionLogs)
	mux.HandleFunc("POST /api/sessions/{id}/kill", handleSessionKill)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	...
	url := fmt.Sprintf("http://localhost:%d", actualPort)
	...
	return serve(listener, mux)
```

Kill handler, `internal/dashboard/server.go:797-812`:
```go
func handleSessionKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("Killing session %s", id)

	binPath := getAgentctlPath()
	cmd := exec.Command(binPath, "kill", id)
	output, err := cmd.CombinedOutput()
	...
```

Logs handler starts `internal/dashboard/server.go:691-697`:
```go
func handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := session.Load(id)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
```
`session.Load(id)` does `filepath.Join(dir, "sessions", id+".json")`, so an id containing `../` (Go's mux unescapes `%2F` in `PathValue`) escapes the data dir.

Session ids: normally 8 hex chars (`internal/session/session.go:41-47`), but tests use ids like `"running"` and `"completed"`, and log-only recovered sessions take their id from the log filename. So validate with a permissive-but-safe pattern, **not** strict hex.

Convention — test hooks are package-level vars, e.g. `internal/dashboard/server.go:36-46`:
```go
	// Kept as a variable so dashboard behavior can be tested without tmux.
	listTmuxSessionsForDashboard = tmux.ListSessions
```
and `resetDashboardState` in `server_test.go:21-63` saves/restores every hook in `t.Cleanup`. Follow that.

The frontend (`internal/dashboard/static/app.js:752`, source `web/src/app.ts`) calls kill with a same-origin `fetch(..., {method: 'POST'})`. Browsers send an `Origin` header on POST, equal to the page origin (e.g. `http://localhost:8080`). The frontend needs **no change**.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Dashboard tests | `go test ./internal/dashboard/ -count=1` | `ok` |
| All tests | `go test ./... -count=1` | all `ok` |

## Scope

**In scope**:
- `internal/dashboard/server.go`
- `internal/dashboard/server_test.go`

**Out of scope**:
- `cmd/kill.go`, `cmd/dashboard.go`, `web/main.go` — no flag or CLI changes. Do not add a `--host`/`--bind` flag.
- `web/src/app.ts`, `internal/dashboard/static/*` — frontend needs no change.
- The log-streaming logic inside `handleSessionLogs` beyond the id validation at the top (a separate plan, 004, rewrites it).
- Authentication tokens — deliberately deferred.

## Git workflow

- Work on the current branch; do not create a branch unless told to.
- Commit message style: single-line imperative, no prefix, no trailing period (e.g. `Harden dashboard against cross-site and LAN access`).
- Do NOT push.

## Steps

### Step 1: Bind loopback only

In `runDashboard`, change the listen address to `fmt.Sprintf("127.0.0.1:%d", port)` and the printed/opened URL to `fmt.Sprintf("http://127.0.0.1:%d", actualPort)`.

In `server_test.go`, `TestRunDashboardBindFailureDoesNotOpen` reserves the port with `net.Listen("tcp", ":0")`. On macOS/BSD a wildcard-bound port does not reliably block a later `127.0.0.1` bind, so change that reservation to `net.Listen("tcp", "127.0.0.1:0")`.

**Verify**: `go test ./internal/dashboard/ -count=1 -run 'TestRunDashboard'` → `ok`

### Step 2: Add id validation and `--` in kill

Add to `server.go`:
```go
var validSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
```
At the top of both `handleSessionLogs` and `handleSessionKill`, after `id := r.PathValue("id")`:
```go
	if !validSessionID.MatchString(id) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
```
Add a test hook next to the others and use it in `handleSessionKill`:
```go
	// Runs `agentctl kill -- <id>`; a variable so tests never exec a real kill.
	runAgentctlKillForDashboard = func(id string) ([]byte, error) {
		return exec.Command(getAgentctlPath(), "kill", "--", id).CombinedOutput()
	}
```
Replace the `exec.Command(binPath, "kill", id)` / `CombinedOutput()` lines with `output, err := runAgentctlKillForDashboard(id)`. Add `runAgentctlKillForDashboard` to the save/restore list in `resetDashboardState`.

**Verify**: `go build ./... && go vet ./internal/dashboard/` → exit 0

### Step 3: Add Host / Origin guard middleware

Add to `server.go`:
```go
// isLoopbackHost reports whether a Host or Origin host (with optional port)
// names this machine's loopback interface.
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// guardLocalRequests rejects DNS-rebinding (non-loopback Host) and
// cross-site state-changing requests (non-loopback Origin on non-GET).
func guardLocalRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !isLoopbackHost(u.Host) {
					http.Error(w, "forbidden origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
```
Note: `runDashboard` has a local variable named `url`; import `net/url` under an alias if it shadows (e.g. `neturl "net/url"`, matching the alias already used in `server_test.go`). An empty `Origin` is allowed so `curl` keeps working; browsers always send `Origin` on cross-site POST.

In `runDashboard`, change `return serve(listener, mux)` to `return serve(listener, guardLocalRequests(mux))`.

**Verify**: `go build ./... && go test ./internal/dashboard/ -count=1` → `ok`

### Step 4: Tests

Add to `server_test.go` (see Test plan). **Verify**: `go test ./internal/dashboard/ -count=1 -v -run 'Guard|Kill|SessionID'` → all new tests PASS.

## Test plan

In `internal/dashboard/server_test.go`, modeled on `TestHandleSessionsPaginationHeadersAndValidation` (direct handler calls with `httptest`):

1. `TestGuardLocalRequestsRejectsForeignHost` — wrap a handler that records calls; request with `req.Host = "evil.example:8080"` → 403, inner not called. `localhost:8080`, `127.0.0.1:8080`, `[::1]:8080` → inner called.
2. `TestGuardLocalRequestsRejectsCrossSitePost` — POST with Host `localhost:8080` and `Origin: https://evil.example` → 403; `Origin: http://localhost:8080` → passes; no Origin → passes; GET with foreign Origin → passes.
3. `TestHandleSessionKillRejectsFlagLikeID` — `resetDashboardState(t)`; stub `runAgentctlKillForDashboard` to record ids; for ids `--all`, `-x`, `../x`, `a/b`, `""`: build request with `req.SetPathValue("id", id)`, call `handleSessionKill` → 400 and stub never called.
4. `TestHandleSessionKillPassesValidID` — id `abc12345` → stub called once with `abc12345`, response 200.
5. `TestHandleSessionLogsRejectsTraversalID` — `req.SetPathValue("id", "../../etc/passwd")` → 400.

## Done criteria

- [ ] `go build ./...` and `go vet ./...` exit 0
- [ ] `go test ./internal/dashboard/ -count=1` → `ok`, includes the 5 new tests
- [ ] `grep -n '":%d"' internal/dashboard/server.go` → no matches
- [ ] `grep -n '"kill", "--", id' internal/dashboard/server.go` → 1 match
- [ ] `grep -n 'guardLocalRequests(mux)' internal/dashboard/server.go` → 1 match
- [ ] `git status` shows only `internal/dashboard/server.go`, `internal/dashboard/server_test.go`, `plans/README.md` modified

## STOP conditions

- The excerpts above don't match the live code.
- Any existing test fails because it relies on a non-loopback Host or listening on all interfaces, beyond the one `TestRunDashboardBindFailureDoesNotOpen` change described in Step 1.
- You find a caller that needs LAN access to the dashboard (e.g. docs describing remote use) — report instead of adding a flag.
- Real session ids in `~/.local/share/agentctl/sessions/` (read-only `ls`) contain characters outside `[A-Za-z0-9_-]`.

## Maintenance notes

- Every new route is covered automatically by `guardLocalRequests`; every new route that takes `{id}` must also check `validSessionID`.
- If remote access is ever wanted, add an explicit `--bind` flag **plus** a token; never go back to `:%d` by default.
- Reviewer: confirm the kill exec has `"--"` before the id and that `resetDashboardState` restores the new hook.
