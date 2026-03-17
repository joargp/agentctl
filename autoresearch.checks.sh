#!/bin/bash
# Checks: build + unit tests must pass
set -euo pipefail
cd "$(dirname "$0")"
go build -o agentctl .
go test ./...
echo "checks passed"
