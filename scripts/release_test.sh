#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
scripts/release.sh v1.2.3 "$out"

while read -r platform; do
  test -x "$out/whatsapp-helper-$platform" || { echo "missing whatsapp-helper-$platform"; exit 1; }
done < scripts/platforms
expected=$(( $(wc -l < scripts/platforms) + 1 ))
test "$(ls "$out" | wc -l)" -eq "$expected" || { echo "unexpected files:"; ls "$out"; exit 1; }
(cd "$out" && shasum -a 256 -c checksums.txt)

host="$(go env GOHOSTOS)-$(go env GOHOSTARCH)"
host="${host/%amd64/x64}"
got="$("$out/whatsapp-helper-$host" --version)"
case "$got" in
  "whatsapp-helper 1.2.3 (protocol "*")") ;;
  *) echo "--version printed '$got', want 'whatsapp-helper 1.2.3 (protocol N)'"; exit 1 ;;
esac

echo "release build ok"
