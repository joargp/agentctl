# Autoresearch Ideas

## Done (18 features)
- JSON mode streaming (core fix: 0→1000+ bytes)
- `--json` flag for raw NDJSON dump
- tee to tmux pane for attach
- 💭 thinking indicators
- Token/cost info at turn boundaries (dump + monitor)
- `status` command (one-line activity summary)
- `--follow/-f` for dump (real-time tailing)
- COST column in ls
- Activity in ls STATUS for running sessions
- `costs` command (aggregate cost tracking)
- Backward compat for plain text logs
- readTail performance optimization
- sync.Once race fix in dump --follow
- Graceful "waiting for output" when log missing
- NAME column in ls (conditional)
- `--since`, `--model`, `--running` filters for ls/costs
- `--clean` flag for kill
- `--summary` flag for condensed dump

## Remaining (nice-to-haves, low priority)
- Use pi control socket (get_summary) for real-time status of sessions that expose one
- Auto-discover new sessions in monitor (--tail flag)
- Show edit tool oldText/newText preview
- Add `--provider` flag to run (currently uses provider/model syntax)
