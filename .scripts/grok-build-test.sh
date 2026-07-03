#!/usr/bin/env bash
set -euo pipefail
cd "D:/weaver-sync/development/personal/projects/agents"

echo "=== go vet ./grok/ ==="
go vet ./grok/

echo "=== go build ./... ==="
go build ./...

echo "=== go test ./grok/ -v ==="
go test ./grok/ -v
