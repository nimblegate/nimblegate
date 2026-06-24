---
name: cidr-host-bits-zero
category: network
subcategory: routing
platform: []
framework: []
severity: WARN
tier: 3
tags: [cloudflare, aws, gcp, kubernetes, ufw, cidr, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.yaml"
    - "**/*.yml"
    - "**/*.tf"
    - "**/*.tfvars"
    - "**/*.json"
    - "**/*.toml"
    - "**/*.conf"
    - "**/*.cfg"
    - "**/*.ini"
    - "**/*.sh"
    - "**/*.bash"
    - "**/ufw*"
dedup-key: "file:line"
pattern: ambiguous-config-value
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-05-20T11:45:47Z
---

# network-safety/cidr-host-bits-zero

Reject IPv4 CIDR strings with host bits set (e.g. `142.132.208.101/24`). Cloudflare, AWS Security Groups, GCP firewall, UFW, and Kubernetes NetworkPolicy all require the network form - Cloudflare even rejects with error code 9109 silently if the input is wrong.

## What this catches

A CIDR like `142.132.208.101/24` describes a *single host on a /24 network*, not the network itself. Most firewall / allowlist surfaces interpret it as a network and reject it because the host bits are non-zero. The fix is mechanical: zero the host bits.

```
142.132.208.101/24  →  142.132.208.0/24   # network
142.132.208.101/32  →  142.132.208.101/32 # single host (this is correct)
```

## How the check works

For every IPv4 CIDR substring found in applicable config files:

1. Parse the CIDR with the standard library
2. Compare the parsed IP against the network IP
3. If they differ, the input has host bits set → BLOCK with the suggested canonical form

Files scanned: `.yaml`, `.yml`, `.tf`, `.tfvars`, `.json`, `.toml`, `.conf`, `.cfg`, `.ini`, `.sh`, `.bash`, plus filenames starting with `ufw`. Files matching `applies-to.files` AND the engine's applicable-file filter are scanned; everything else is skipped.

## Fix

Replace the input with the network form. For an individual IP (not a range), use `/32` explicitly:

```bash
# WRONG - CF rejects with code 9109
ufw allow from 142.132.208.101/24

# RIGHT - pick one
ufw allow from 142.132.208.0/24        # whole /24 network
ufw allow from 142.132.208.101/32      # single host
```

## Suppressing intentional cases

If a file legitimately needs a non-network CIDR (test fixture, documentation example), suppress per-line or per-file:

```yaml
# appframes:disable-next-line network-safety/cidr-host-bits-zero
example_cidr: 1.2.3.4/24

# appframes:disable network-safety/cidr-host-bits-zero
# (entire file suppressed below this point)
```

For project-wide vendored configs, add a whitelist entry.

## Generalizes to

Any firewall / IP-allowlist surface that takes CIDRs:

- Cloudflare API token IP allowlists
- AWS Security Groups / NACLs
- GCP firewall rules
- Kubernetes NetworkPolicy CIDRs
- Hetzner / DigitalOcean cloud firewalls
- UFW / iptables / nftables
- Tailscale ACL grants
- Wireguard `AllowedIPs`

The frame fires identically on all of them - same parse, same fix.
