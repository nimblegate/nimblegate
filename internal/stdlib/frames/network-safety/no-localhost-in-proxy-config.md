---
name: no-localhost-in-proxy-config
category: network
subcategory: proxy-config
platform: []
framework: []
severity: WARN
tier: 1
tags: [cloudflare, cloudflared, nginx, caddy, haproxy, traefik, ipv6, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/cloudflared*.yml"
    - "**/cloudflared*.yaml"
    - "**/nginx.conf"
    - "**/*.conf"
    - "**/*.cfg"
    - "**/Caddyfile"
    - "**/cloudflared/**/*.yaml"
    - "**/nginx/**/*.conf"
    - "**/caddy/**"
    - "**/haproxy/**"
    - "**/traefik/**/*.yaml"
dedup-key: "file:line"
pattern: ambiguous-config-value
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-05-20T14:37:33Z
---

# network-safety/no-localhost-in-proxy-config

Reject reverse-proxy config files that name `localhost` as the upstream target. Modern Go-based proxies (cloudflared, anything using the Go net resolver) try IPv6 (`[::1]`) before IPv4 (`127.0.0.1`) on Linux. If the destination service binds only `0.0.0.0:<port>` and not `[::]:<port>`, every request fails with `connection refused` on the host side and an opaque error on the client side.

Typical failure shape: `service: ssh://localhost:22` in `/etc/cloudflared/config.yml` makes cloudflared dial `[::1]:22` first. sshd is bound only on `0.0.0.0:22`. Every SSH-via-tunnel attempt fails with `dial tcp [::1]:22: connect: connection refused` on the host and `websocket: bad handshake` on the client laptop - with no useful signal on the client side. Hours of debugging until the IPv6 resolution-order theory lands.

## What this catches

`localhost` references in upstream / proxy-pass / reverse_proxy directives across the common reverse-proxy config formats:

| Proxy | Pattern that fires |
|-------|---------------------|
| cloudflared | `service: <scheme>://localhost:<port>` |
| nginx | `proxy_pass http(s)://localhost...` |
| nginx upstream | `server localhost:<port>;` |
| Caddy | `reverse_proxy localhost:<port>` |
| HAProxy | `server <name> localhost:<port>` |
| Traefik | `url = "http(s)://localhost:<port>"` |

## Fix

Replace `localhost` with the literal loopback IP:

```yaml
# WRONG (cloudflared) - Go resolver picks IPv6 first
service: ssh://localhost:22

# RIGHT - pin the address family
service: ssh://127.0.0.1:22    # IPv4 listener
service: ssh://[::1]:22        # IPv6 listener (verify with `ss -tlnp` first)
```

```nginx
# WRONG
proxy_pass http://localhost:8080;

# RIGHT
proxy_pass http://127.0.0.1:8080;
```

```caddyfile
# WRONG
reverse_proxy localhost:3000

# RIGHT
reverse_proxy 127.0.0.1:3000
```

If you genuinely need dual-stack (rare for loopback), bind the destination service on `[::]:<port>` too and choose `127.0.0.1` here. The point: pin the family at config time, don't let the resolver pick.

## Suppressing intentional cases

For documentation comments / example configs that intentionally show `localhost`:

```yaml
# appframes:disable-next-line network-safety/no-localhost-in-proxy-config
# Example: service: http://localhost:8080
```

## Generalizes to

Any service-config file that takes an upstream hostname:

- Cloudflare Tunnel (`cloudflared`)
- nginx `proxy_pass` / `upstream server`
- Caddy `reverse_proxy`
- HAProxy `server`
- Traefik service URLs
- Docker `host.docker.internal` is a similar trap; pin to `host-gateway` or a literal IP
- Redis `bind localhost` vs `bind 127.0.0.1`
- Postgres `listen_addresses = 'localhost'` vs `'127.0.0.1'`

The frame currently covers reverse-proxy configs only; expanding to Redis/Postgres configs is a future addition.
