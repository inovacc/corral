#!/usr/bin/env bash
set -euo pipefail
cd "D:/weaver-sync/development/personal/projects/agents"
echo "== go vet ./qwen/ =="
go vet ./qwen/
echo "== go build ./qwen/ =="
go build ./qwen/
echo "== go test ./qwen/ =="
go test ./qwen/ -run 'TestIsQuotaExhausted|TestRetryAfter' -v
