#!/usr/bin/env bash
set -euo pipefail
cd "D:/weaver-sync/development/personal/projects/agents"
echo "== gofmt =="
gofmt -l kimi/
echo "== go vet =="
go vet ./kimi/
echo "== go build (all) =="
go build ./...
echo "== go test kimi + all =="
go test ./kimi/ ./all/
echo "OK"
