#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

test -z "$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
go vet ./...
go test -race ./...
scripts/release_test.sh
scripts/open_bump_pr_test.sh
