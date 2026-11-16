#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

module=go.mau.fi/whatsmeow
version="$(go list -m -f '{{.Version}}' "$module@${1:-main}")"

go get "$module@$version"
go mod tidy
echo "$version"
