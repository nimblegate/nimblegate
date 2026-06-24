#!/usr/bin/env bash
# Rebuild the local nbg-eval container from current HEAD, prune build cache,
# and recreate the container fresh.
#
# Why prune? Each rebuild adds layers + build-cache mounts. Without prune,
# /mnt/extra (or wherever Docker's data-root lives) fills up - typically
# 200 MB to 2 GB per rebuild - and the container's auth DB then fails to
# write with a SQLite I/O error. Running prune after each build keeps the
# data-root flat at the cost of ~30 s on subsequent rebuilds (the layers
# get re-fetched, but go module cache + apk cache stay warm via build args).
#
# Volumes (nbg-eval-ssh / nbg-eval-repos / nbg-eval-cfg) persist across
# container recreation - your authorized SSH keys, registered repos, and
# policy survive.
#
# Usage:
#   bash scripts/nbg-rebuild.sh             # build + prune + recreate
#   bash scripts/nbg-rebuild.sh --no-prune  # skip the prune step
#
# Run from the repo root.

set -euo pipefail

PRUNE=1
for arg in "$@"; do
  case "$arg" in
    --no-prune) PRUNE=0 ;;
    -h|--help)
      sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown arg: $arg" >&2
      exit 2
      ;;
  esac
done

# Repo root sanity check.
if [ ! -f Dockerfile ] || [ ! -d cmd/nimblegate ]; then
  echo "run this from the repo root (Dockerfile + cmd/nimblegate/ expected)" >&2
  exit 2
fi

VERSION=$(git rev-parse --short HEAD)
echo "==> Building nimblegate:eval-alpine @ ${VERSION}"
docker build --build-arg VERSION="${VERSION}" -t nimblegate:eval-alpine .

if [ "${PRUNE}" = "1" ]; then
  echo "==> Pruning Docker build cache + dangling images"
  docker builder prune -f >/dev/null
  docker image prune -f >/dev/null
  echo "    data-root usage now:"
  df -h "$(docker info --format '{{.DockerRootDir}}' 2>/dev/null)" 2>/dev/null \
    | awk 'NR==1 || NR==2' \
    | sed 's/^/      /'
fi

echo "==> Recreating nbg-eval container"
docker rm -f nbg-eval >/dev/null 2>&1 || true
docker run -d \
  --name nbg-eval \
  --restart unless-stopped \
  -p 0.0.0.0:2222:22 \
  -p 127.0.0.1:7900:7900 \
  -v nbg-eval-ssh:/srv/gateway/ssh \
  -v nbg-eval-repos:/srv/gateway/repos \
  -v nbg-eval-cfg:/srv/gateway/cfg \
  nimblegate:eval-alpine >/dev/null

sleep 2
RUNNING=$(docker exec nbg-eval nimblegate version 2>/dev/null || true)
if [ -z "${RUNNING}" ]; then
  echo "nimblegate: container is up but the gateway binary inside it isn't responding - see: docker logs nbg-eval" >&2
  exit 1
fi
echo "==> ${RUNNING}"
echo "    dashboard: http://localhost:7900"
echo "    ssh push:  ssh://git@localhost:2222/<repo>.git"
