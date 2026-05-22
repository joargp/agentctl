# agentctl Dashboard

A beautiful, responsive browser-based dashboard to inspect and stream active/completed `agentctl` sessions in real-time.

Features:
- **Real-time Log Streaming**: Uses Server-Sent Events (SSE) to tail NDJSON logs from active tmux agent sessions as they execute.
- **Interactive Formatting**: Collapsible thinking processes, elegant action blocks for tool calls, and auto-scrolled logs.
- **Session Management**: Full list of historical and running sessions with search/filtering, turn count, cost tracking, and age.
- **Intervention Control**: Live session termination ("Kill") from the UI.
- **Zero-Dependency Runtime**: Embedded static assets inside a single compiled Go binary.

## Run the Dashboard

To start the dashboard directly:

```bash
go run web/main.go --port 8080
```

Then open your browser to: **http://localhost:8080**

## Build the Binary

To compile the self-contained dashboard binary:

```bash
go build -o web/dashboard web/main.go
```

The compiled binary `web/dashboard` contains all static CSS, HTML, and JS compiled directly into it.

## Development

If you want to edit the frontend code:

1. Edit the TypeScript code in `web/src/app.ts`.
2. Compile the TypeScript code to JavaScript:
   ```bash
   tsc -p web/tsconfig.json
   ```
3. Run/rebuild `web/main.go` to serve the updated JS.
