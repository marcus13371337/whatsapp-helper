#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

tag="${1:?usage: scripts/release.sh <tag, e.g. v0.1.0> [output folder, default dist]}"
out="${2:-dist}"
version="${tag#v}"

mkdir -p "$out"
rm -f "$out"/whatsapp-helper-* "$out"/checksums.txt
while read -r platform; do
  goos="${platform%-*}"
  goarch="${platform#*-}"
  [ "$goarch" = x64 ] && goarch=amd64
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$version" \
    -o "$out/whatsapp-helper-$platform" .
done < scripts/platforms
(cd "$out" && shasum -a 256 whatsapp-helper-* > checksums.txt)
