#!/usr/bin/env bash
# Build the binary, build the test image, and run the scoped-access
# boundary assertions in a throwaway container. Requires Docker + a Go toolchain.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(git -C "$HERE" rev-parse --show-toplevel)"
cd "$ROOT"

echo "== building static binary =="
CGO_ENABLED=0 go build -o "$HERE/nimblegate" ./cmd/nimblegate

echo "== building test image =="
docker build -q -t nbg-scoped-access-test "$HERE" >/dev/null

echo "== running scoped-access assertions in container =="
docker run --rm \
  -v "$HERE/nimblegate:/usr/local/bin/nimblegate:ro" \
  -v "$HERE/scoped-test.sh:/scoped-test.sh:ro" \
  nbg-scoped-access-test bash /scoped-test.sh
rc=$?

rm -f "$HERE/nimblegate"
exit $rc
