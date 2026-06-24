#!/usr/bin/env bash
# Build the static binary, build the test base image, and run the relay
# privilege-boundary assertions inside a throwaway container. Run from anywhere
# in the repo. Requires Docker + a Go toolchain.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(git -C "$HERE" rev-parse --show-toplevel)"
cd "$ROOT"

echo "== building static binary =="
CGO_ENABLED=0 go build -o "$HERE/nimblegate" ./cmd/nimblegate

echo "== building test image =="
docker build -q -t nbg-relay-boundary-test "$HERE" >/dev/null

echo "== running boundary assertions in container =="
# --rm: throwaway. Mount the freshly built binary + script read-only.
docker run --rm \
  -v "$HERE/nimblegate:/usr/local/bin/nimblegate:ro" \
  -v "$HERE/boundary-test.sh:/boundary-test.sh:ro" \
  nbg-relay-boundary-test bash /boundary-test.sh
rc=$?

rm -f "$HERE/nimblegate"
exit $rc
