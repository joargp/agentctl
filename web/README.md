# agentctl Dashboard

A beautiful, responsive browser-based dashboard to inspect and stream active/completed `agentctl` sessions in real-time.

Features:
- **Real-time Log Streaming**: Uses Server-Sent Events (SSE) to tail NDJSON logs from active tmux agent sessions as they execute.
- **Interactive Formatting**: Collapsible thinking processes, elegant action blocks for tool calls, and auto-scrolled logs.
- **Session Management**: Full list of historical and running sessions with search/filtering, turn count, cost tracking, and age.
- **Intervention Control**: Live session termination ("Kill") from the UI.
- **Zero-Dependency Runtime**: Embedded static assets inside a single compiled Go binary.

## Run the Dashboard

Primary entry point:

```bash
agentctl dashboard --port 8080
```

Development wrapper:

```bash
go run web/main.go --port 8080
```

Then open your browser to: **http://localhost:8080**

## Development

If you want to edit the frontend code:

1. Edit the TypeScript code in `web/src/app.ts`.
2. Compile the TypeScript code to JavaScript:
   ```bash
   tsc -p web/tsconfig.json
   ```
   This writes `internal/dashboard/static/app.js` for the embedded dashboard assets.
3. Run `agentctl dashboard --port 8080` or `go run web/main.go --port 8080` to serve the updated JS.

## Wrapper Build

If you want a development-only wrapper binary:

```bash
go build -o /tmp/agentctl-dashboard ./web
```

The installed `agentctl` binary already includes the dashboard command and embedded assets.
