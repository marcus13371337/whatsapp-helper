#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

test -z "$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -o /dev/null .
