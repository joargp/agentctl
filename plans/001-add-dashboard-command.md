# Plan 001: Add a first-class `agentctl dashboard` command

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 7dc29f2..HEAD -- README.md web/README.md web/main.go web/static web/tsconfig.json cmd internal/dashboard`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `7dc29f2`, 2026-06-12

## Why this matters

The repository already contains a useful browser dashboard for inspecting, streaming, and killing `agentctl` sessions, but it is only reachable as `go run web/main.go --port 8080`. Users who install `agentctl` via `go install github.com/joargp/agentctl@latest` do not get a discoverable `agentctl dashboard` entry point, even though the main README positions agentctl as both a runner and monitoring tool. This plan promotes the dashboard into the installed CLI while preserving the existing `web/main.go` development entry point.

## Current state

Relevant files and roles:

- `main.go` — root binary entry point; currently delegates to the Cobra CLI package.
- `cmd/root.go` — root Cobra command and common startup directory setup.
- `web/main.go` — standalone dashboard server binary; owns embedded static assets and HTTP handlers.
- `web/static/index.html` and `web/static/app.js` — dashboard assets embedded by `web/main.go`.
- `web/src/app.ts` and `web/tsconfig.json` — TypeScript source and compiler output location for `web/static/app.js`.
- `web/README.md` — dashboard-specific docs; currently documents `go run web/main.go` only.
- `README.md` — main user docs and command table; currently omits the dashboard.

Current root CLI entry point (`main.go:1-5`):

```go
package main

import "github.com/joargp/agentctl/cmd"

func main() {
	cmd.Execute()
}
```

Current root Cobra setup (`cmd/root.go:12-20`):

```go
var rootCmd = &cobra.Command{
	Use:          "agentctl",
	Short:        "Run and monitor pi coding agent sessions",
	Version:      currentVersion(),
	SilenceUsage: true, // don't print usage on runtime errors
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return ensureDirs()
	},
}
```

Current dashboard standalone server shape (`web/main.go:20-35`, `web/main.go:218-257`):

```go
//go:embed static/*
var staticFiles embed.FS

var (
	cacheMutex     sync.RWMutex
	sessionCache   = make(map[string]APISession)
	loadedSessions = make(map[string]*session.Session)
)

type APISession struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	Task       string    `json:"task"`
	Cwd        string    `json:"cwd"`
	StartedAt  time.Time `json:"started_at"`
	Status     string    `json:"status"` // "running" or "done"
	Turns      int       `json:"turns"`
	TotalCost  float64   `json:"total_cost"`
	LastState  string    `json:"last_state"`
	LastDetail string    `json:"last_detail"`
}

func main() {
	port := flag.Int("port", 8080, "port to run the server on")
	flag.Parse()

	// Start progressive background indexing
	startIndexingBackground()

	// 1. Root and Static Files Routes
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// ...
	})

	// 2. API Routes
	http.HandleFunc("GET /api/sessions", handleSessions)
	http.HandleFunc("GET /api/sessions/{id}/logs", handleSessionLogs)
	http.HandleFunc("POST /api/sessions/{id}/kill", handleSessionKill)

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("agentctl dashboard running on http://localhost:%d\n", *port)
	log.Fatal(http.ListenAndServe(addr, nil))
}
```

Current dashboard docs (`web/README.md:12-28`):

```markdown
## Run the Dashboard

To start the dashboard directly:

```bash
go run web/main.go --port 8080
```

Then open your browser to: **http://localhost:8080**
```

Current main command table (`README.md:26-37`) has no dashboard row:

```markdown
| Command | Description |
|---|---|
| `run --model <m> (--task <t>|--task-file <path>)` | Spawn a pi agent session |
| `ls` | List sessions with status, age, and cost |
| `status <id>` | One-line summary (thinking, running bash, writing...) |
| `monitor [id...]` | Stream live labeled output |
| `dump <id> [-n] [-f] [--json] [--summary]` | Print/follow rendered output (or raw JSON) |
| `attach <id>` | Attach terminal for manual intervention |
| `costs` | Show per-session and total API costs |
| `kill <id> / --all` | Kill session(s), preserve logs |
```

Repo conventions to match:

- Commands live in `cmd/<command>.go`, register themselves in `init()`, and use Cobra. Follow `cmd/version.go` for a small command and `cmd/run.go` for flags.
- Public CLI text should keep stdout/stderr behavior predictable. For this server command, printing the listening URL to stdout is acceptable because the command is long-running and interactive.
- The repo uses single-line imperative commit messages, e.g. `Add agentctl browser dashboard app` and `Simplify README and move notifier details to docs`.
- Static dashboard assets are generated from TypeScript source; if `app.ts` changes, `web/static/app.js` must stay in sync. In this plan, you should not change dashboard UI behavior.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Drift check | `git diff --stat 7dc29f2..HEAD -- README.md web/README.md web/main.go web/static web/tsconfig.json cmd internal/dashboard` | No output, or only changes you have explicitly reconciled with this plan |
| Go tests | `go test ./...` | exit 0; all packages pass |
| Root build | `go build -o /tmp/agentctl .` | exit 0 and writes `/tmp/agentctl` |
| Web wrapper build | `go build -o /tmp/agentctl-dashboard ./web` | exit 0 and writes `/tmp/agentctl-dashboard` |
| TypeScript typecheck | `tsc -p web/tsconfig.json --noEmit` | exit 0, no errors |
| CLI help smoke test | `/tmp/agentctl dashboard --help` | exit 0 and shows dashboard help including `--port` |

## Scope

**In scope** (the only source/docs files you should modify):

- `README.md`
- `web/README.md`
- `web/main.go`
- `web/tsconfig.json`
- `web/static/index.html` and `web/static/app.js` (move these into the new package; do not rewrite behavior)
- `cmd/dashboard.go` (create)
- `cmd/dashboard_test.go` (create)
- `internal/dashboard/server.go` (create)
- `internal/dashboard/static/index.html` and `internal/dashboard/static/app.js` (create by moving/copying current assets)
- `plans/README.md` (status update only when finished)

**Out of scope** (do NOT touch, even though they look related):

- Dashboard UI behavior in `web/src/app.ts` beyond any path/output metadata required by this plan.
- Session launch/re-run from the dashboard; that is a separate future feature.
- JSON output for `ls`, `status`, or `costs`; that is a separate future feature.
- Notification semantics and `cmd/agentctl-notify-codex`.
- Release workflow changes unless the root binary fails to include the dashboard command. The intended result is that the existing root `agentctl` release binary includes `agentctl dashboard`; do not add a separate dashboard release artifact.
- Existing untracked local files such as `.agents/` or `skills-lock.json`; ignore them.

## Git workflow

- Branch: `advisor/001-dashboard-command` if you create a branch.
- Commit message style: single-line imperative mood, no conventional prefix. Example: `Add dashboard command`.
- Do NOT push or open a PR unless the operator explicitly asks.

## Steps

### Step 1: Move the dashboard server into an importable package

Create `internal/dashboard/server.go` and move the dashboard implementation from `web/main.go` into package `dashboard`.

Required shape:

- Package name: `dashboard`.
- Keep the embedded assets in this package with:

  ```go
  //go:embed static/*
  var staticFiles embed.FS
  ```

- Expose:

  ```go
  func Run(port int) error
  ```

  `Run` should:
  - call `startIndexingBackground()`;
  - register the same routes currently in `web/main.go`;
  - listen on `fmt.Sprintf(":%d", port)`;
  - print `agentctl dashboard running on http://localhost:%d\n` before serving;
  - return the `http.ListenAndServe` error instead of calling `log.Fatal`.

- Prefer a local `mux := http.NewServeMux()` inside `Run` instead of using package-global `http.HandleFunc`. Register the current handlers on that mux and pass it to `http.ListenAndServe(addr, mux)`. This avoids surprises when the dashboard is run inside the larger CLI process.
- Preserve all current helper functions and types (`APISession`, `deriveLastActivity`, `handleSessions`, `handleSessionLogs`, `handleSessionKill`, etc.) in the new package unless a compile error requires a name adjustment.
- Remove the `flag` and `log` imports from the moved server package if they are no longer used.

Move or copy the current static assets so the new embed path exists:

- from `web/static/index.html` to `internal/dashboard/static/index.html`
- from `web/static/app.js` to `internal/dashboard/static/app.js`

After this step, `internal/dashboard/server.go` owns the HTTP server implementation and embedded assets.

**Verify**: `go test ./internal/dashboard` → exit 0. It is acceptable if the package reports `[no test files]` at this point.

### Step 2: Preserve the standalone dashboard wrapper

Replace `web/main.go` with a small wrapper that keeps the existing development command working.

Target shape:

```go
package main

import (
	"flag"
	"log"

	"github.com/joargp/agentctl/internal/dashboard"
)

func main() {
	port := flag.Int("port", 8080, "port to run the server on")
	flag.Parse()

	if err := dashboard.Run(*port); err != nil {
		log.Fatal(err)
	}
}
```

Do not change the semantics of `go run web/main.go --port 8080` except that the implementation now lives in `internal/dashboard`.

**Verify**: `go build -o /tmp/agentctl-dashboard ./web` → exit 0.

### Step 3: Add the `agentctl dashboard` Cobra command

Create `cmd/dashboard.go`.

Required behavior:

- Command use: `dashboard`.
- Short description: `Run the browser dashboard`.
- Long description should mention that it serves the local browser dashboard for inspecting, streaming, and killing agentctl sessions.
- Add `--port` flag with default `8080` and help text like `port to run the dashboard server on`.
- Register with `rootCmd.AddCommand(dashboardCmd)` in `init()`.
- The command should call `dashboard.Run(dashboardPort)` and return its error.

For testability, use an overridable package-level function variable:

```go
var runDashboard = dashboard.Run
```

Then `RunE` calls `runDashboard(dashboardPort)`. This matches the repo's simple command style while letting tests avoid starting a blocking HTTP server.

**Verify**: `go build -o /tmp/agentctl .` → exit 0.

### Step 4: Add command tests

Create `cmd/dashboard_test.go` with at least these tests:

1. A test that stubs `runDashboard`, sets `dashboardPort` to a non-default value (for example `9191`), calls `dashboardCmd.RunE(dashboardCmd, nil)`, and asserts the stub received that port.
2. A test that verifies the command has a `port` flag with default value `8080`.

Use existing command tests as style examples:

- `cmd/version_test.go` shows testing a small Cobra command by overriding output and calling the command function.
- `cmd/run_test.go` shows restoring package-level variables after tests.

Make sure each test restores any modified package-level variables with `defer` so other command tests are not polluted.

**Verify**: `go test ./cmd` → exit 0.

### Step 5: Update TypeScript output path and docs for the moved assets

Update `web/tsconfig.json` so compiling `web/src/app.ts` writes generated JavaScript to the new embed directory:

```json
"outDir": "../internal/dashboard/static"
```

Keep `rootDir`, `target`, `module`, and strictness settings unchanged.

Update `web/README.md`:

- Make `agentctl dashboard --port 8080` the primary way to run the dashboard.
- Keep `go run web/main.go --port 8080` as a development fallback.
- Update development instructions so TypeScript compilation points out that `tsc -p web/tsconfig.json` writes `internal/dashboard/static/app.js`.
- Remove or revise the claim that `go build -o web/dashboard web/main.go` is the primary self-contained dashboard binary. The dashboard is now included in the root `agentctl` binary; the wrapper build may remain documented only as a development helper.

Update `README.md`:

- Add a command-table row for `dashboard [--port 8080]`.
- Add a short example near the monitoring/reading-output section:

  ```bash
  agentctl dashboard --port 8080
  ```

  and state that it opens a local browser dashboard for session history, live logs, and killing running sessions.

**Verify**: `tsc -p web/tsconfig.json --noEmit` → exit 0, no TypeScript errors.

### Step 6: Final end-to-end verification

Run all verification commands:

1. `go test ./...` → exit 0.
2. `go build -o /tmp/agentctl .` → exit 0.
3. `/tmp/agentctl dashboard --help` → exit 0 and help includes `Run the browser dashboard` plus `--port`.
4. `go build -o /tmp/agentctl-dashboard ./web` → exit 0.
5. `tsc -p web/tsconfig.json --noEmit` → exit 0.

Then inspect the final diff:

- `git diff --stat` should show only files in this plan's in-scope list.
- `git diff -- README.md web/README.md cmd/dashboard.go web/main.go internal/dashboard/server.go web/tsconfig.json` should show no unrelated UI or session-behavior changes.

**Verify**: all five commands above pass and the diff is limited to in-scope files.

## Test plan

New tests:

- `cmd/dashboard_test.go::TestDashboardCommandRunsConfiguredPort` — proves the Cobra command invokes the dashboard runner with the configured port without starting a real server.
- `cmd/dashboard_test.go::TestDashboardCommandHasDefaultPort` — proves the command exposes `--port` with default `8080`.

Existing tests to rely on:

- `go test ./cmd` for command wiring.
- `go test ./...` for full repository regression coverage.

Manual/smoke verification:

- `/tmp/agentctl dashboard --help` should show the new command and `--port` flag.
- Do not start a long-running dashboard server in automated tests.

## Done criteria

ALL must hold:

- [ ] `agentctl dashboard --help` is available from the root binary and documents `--port`.
- [ ] `go run web/main.go --port 8080` still compiles/runs via the wrapper path.
- [ ] Dashboard assets are embedded from `internal/dashboard/static/*` into the root `agentctl` binary.
- [ ] `web/tsconfig.json` writes dashboard JavaScript to the embed directory used by the root binary.
- [ ] `go test ./...` exits 0.
- [ ] `go build -o /tmp/agentctl .` exits 0.
- [ ] `go build -o /tmp/agentctl-dashboard ./web` exits 0.
- [ ] `tsc -p web/tsconfig.json --noEmit` exits 0.
- [ ] No files outside the in-scope list are modified, except for `plans/README.md` status update.
- [ ] `plans/README.md` status row for this plan is updated to `DONE` or `BLOCKED` with a reason.

## STOP conditions

Stop and report back (do not improvise) if:

- The dashboard server in `web/main.go` no longer matches the structure shown in the Current state excerpts.
- Moving the static assets would require changing dashboard UI behavior in `web/src/app.ts` beyond the TypeScript output path.
- The root `agentctl` binary cannot embed the dashboard assets without introducing a separate release artifact.
- A step's verification fails twice after a reasonable fix attempt.
- The change appears to require touching notification code, session storage formats, tmux process management, or the release workflow.

## Maintenance notes

- Future dashboard features should live in `internal/dashboard` if they are part of the installed `agentctl` binary. Keep `web/main.go` as a thin development wrapper only.
- If the dashboard UI changes, ensure `web/src/app.ts` and generated `internal/dashboard/static/app.js` stay in sync.
- Reviewers should pay close attention to `http.ServeMux` usage: the imported dashboard package should not register routes on `http.DefaultServeMux` as a side effect.
- This plan intentionally does not add dashboard session launch/re-run. That should be designed separately because it changes the dashboard from inspect/kill into a broader command-execution surface.
