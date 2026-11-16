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

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) host=darwin-arm64 ;;
  Darwin-x86_64) host=darwin-x64 ;;
  Linux-x86_64) host=linux-x64 ;;
  Linux-aarch64) host=linux-arm64 ;;
  *) echo "no release build runs on $(uname -s)-$(uname -m)"; exit 1 ;;
esac
got="$("$out/whatsapp-helper-$host" --version)"
want="whatsapp-helper 1.2.3 (protocol 1)"
test "$got" = "$want" || { echo "--version printed '$got', want '$want'"; exit 1; }

echo "release build ok"
