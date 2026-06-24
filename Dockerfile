# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0
#
# nimblegate combined container - git push endpoint (sshd) + dashboard (HTTP),
# both supervised by s6-overlay v3 (auto-restart on failure, per-service logs).
#
# Build from repo root:
#   docker build -t nimblegate:eval-alpine .
# The binary, the module path, and the public brand are all `nimblegate`.

ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.20
ARG S6_OVERLAY_VERSION=3.2.0.2

# ---------- build stage: compile the nimblegate binary ----------
FROM golang:${GO_VERSION}-alpine AS build

# git: used for `git rev-parse` during version stamping if invoked.
RUN apk add --no-cache git

WORKDIR /src

# cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# source
COPY . .

# statically-linked binary; CGO off so musl/glibc both work.
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags="-s -w -X nimblegate/internal/version.Version=${VERSION}" \
      -o /out/nimblegate ./cmd/nimblegate

# ---------- runtime stage: alpine + sshd + s6-overlay + nimblegate ----------
FROM alpine:${ALPINE_VERSION}
ARG S6_OVERLAY_VERSION
ARG TARGETARCH

LABEL org.opencontainers.image.title="nimblegate" \
      org.opencontainers.image.description="Deterministic policy gate for AI-agent git pushes - sshd + dashboard in one container" \
      org.opencontainers.image.source="https://github.com/nimblegate/nimblegate" \
      org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"

# Runtime deps:
#   openssh-server  - accepts git push over SSH
#   git             - provides git-shell + git-receive-pack + git-upload-pack
#   ca-certificates - needed by the post-receive relay to validate HTTPS upstreams
# (s6-overlay handles PID-1, signal forwarding, and zombie reaping itself - no
#  tini needed. busybox wget is built into the alpine base; the s6-overlay
#  tarball download below uses it without an extra install.)
RUN apk add --no-cache openssh-server git ca-certificates \
 && addgroup -S -g 1000 git \
 && adduser  -S -u 1000 -G git -h /home/git -s /usr/bin/git-shell git \
 && sed -i -E 's/^(git:)[!*]+/\1*/' /etc/shadow \
 && mkdir -p /srv/gateway/repos /srv/gateway/cfg /srv/gateway/ssh \
 && rm -rf /home/git \
 && ln -s /srv/gateway/repos /home/git \
 && chown -R git:git /srv/gateway \
 && chown -h git:git /home/git

# Install s6-overlay v3. Multi-arch via TARGETARCH (set by buildkit).
RUN set -eu \
 && case "${TARGETARCH:-amd64}" in \
      amd64)  S6_ARCH=x86_64 ;; \
      arm64)  S6_ARCH=aarch64 ;; \
      *)      echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && wget -qO /tmp/s6-noarch.tar.xz "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz" \
 && wget -qO /tmp/s6-arch.tar.xz   "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-${S6_ARCH}.tar.xz" \
 && tar -C / -Jxpf /tmp/s6-noarch.tar.xz \
 && tar -C / -Jxpf /tmp/s6-arch.tar.xz \
 && rm -f /tmp/s6-noarch.tar.xz /tmp/s6-arch.tar.xz

# Shell scripts invoked by s6 oneshot up files (oneshot `up` files use execline
# syntax in s6-overlay v3, so the shell logic lives in separate .sh files
# called from one-line execline commands in the `up` file).
COPY deploy/container/scripts /etc/s6-overlay/scripts
RUN chmod +x /etc/s6-overlay/scripts/*.sh

# Service definitions (sshd, dashboard, init-host-keys) for s6-overlay.
COPY deploy/container/s6-rc.d /etc/s6-overlay/s6-rc.d

# Operator helper scripts (nbg-status, nbg-restart, nbg-logs, nbg-reset,
# nbg-regen-keys). Invoked via: docker exec <container> nbg-<cmd>.
COPY deploy/container/bin /usr/local/bin
RUN chmod +x /usr/local/bin/nbg-*

# The nimblegate binary itself.
COPY --from=build /out/nimblegate /usr/local/bin/nimblegate

EXPOSE 22 7900
VOLUME ["/srv/gateway/repos", "/srv/gateway/cfg", "/srv/gateway/ssh"]

# s6 supervision verbosity - 2 surfaces per-service exit/restart events in
# docker logs (default 1 omits them). Combined with the per-service `finish`
# scripts, this makes crash loops visible:
#   docker logs <container> | grep nbg-supervise
ENV S6_VERBOSITY=2

# s6-overlay's /init handles tini-style PID 1 duties: signal forwarding,
# zombie reaping, supervised service startup, and graceful shutdown.
ENTRYPOINT ["/init"]
