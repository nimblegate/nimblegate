// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// proxyLocalhostShape is one detection pattern + a human label. Each
// shape covers the equivalent footgun in a different proxy config syntax.
type proxyLocalhostShape struct {
	Label   string
	Pattern *regexp.Regexp
}

var proxyLocalhostShapes = []proxyLocalhostShape{
	// cloudflared: `service: ssh://localhost:22` or `service: http://localhost:8080`
	{
		Label:   "cloudflared service: scheme://localhost:port",
		Pattern: regexp.MustCompile(`(?i)\bservice\s*:\s*[a-z][a-z0-9+.-]*://localhost(:\d+)?\b`),
	},
	// nginx: `proxy_pass http://localhost:8080;`
	{
		Label:   "nginx proxy_pass http(s)://localhost",
		Pattern: regexp.MustCompile(`(?i)\bproxy_pass\s+https?://localhost(:\d+)?`),
	},
	// nginx upstream block server line: `server localhost:8080;`
	{
		Label:   "nginx upstream `server localhost:port`",
		Pattern: regexp.MustCompile(`(?im)^\s*server\s+localhost(:\d+)?\s*;`),
	},
	// Caddy: `reverse_proxy localhost:8080`
	{
		Label:   "Caddy reverse_proxy localhost:port",
		Pattern: regexp.MustCompile(`(?i)\breverse_proxy\s+localhost(:\d+)?\b`),
	},
	// HAProxy: `server name localhost:8080`
	{
		Label:   "HAProxy `server <name> localhost:port`",
		Pattern: regexp.MustCompile(`(?i)\bserver\s+\S+\s+localhost(:\d+)?\b`),
	},
	// Traefik: `url = "http://localhost:8080"` inside service config
	{
		Label:   "Traefik service url http(s)://localhost",
		Pattern: regexp.MustCompile(`(?i)\burl\s*=\s*"https?://localhost(:\d+)?"`),
	},
}

// proxyLocalhostApplicableFile returns true when the file is a known
// reverse-proxy config format. Hostnames in arbitrary code/comments
// are out of scope - the false-positive cost on /docs and /README is
// higher than the value.
func proxyLocalhostApplicableFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	// Known config filenames (no extension or non-standard).
	if base == "nginx.conf" || base == "caddyfile" || base == "haproxy.cfg" {
		return true
	}
	if strings.HasPrefix(base, "cloudflared") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".conf", ".cfg":
		return true
	}
	// YAML files under directories that conventionally hold reverse-proxy
	// configs are also scanned.
	if ext == ".yaml" || ext == ".yml" {
		parts := strings.Split(filepath.ToSlash(path), "/")
		for _, p := range parts {
			pl := strings.ToLower(p)
			if pl == "cloudflared" || pl == "traefik" || pl == "nginx" || pl == "caddy" || pl == "haproxy" {
				return true
			}
		}
	}
	return false
}

const proxyLocalhostDisableMarker = "appframes:disable network/no-localhost-in-proxy-config"
const proxyLocalhostDisableLineMarker = "appframes:disable-next-line network/no-localhost-in-proxy-config"
const proxyLocalhostMaxFileBytes = 1 << 20 // 1 MiB

// NoLocalhostInProxyConfig scans reverse-proxy config files for upstream
// definitions that name `localhost` instead of a literal loopback IP
// (127.0.0.1 or [::1]).
//
// The footgun: Go's net resolver (used by cloudflared and most modern
// proxies) returns IPv6 addresses first on Linux. `service: ssh://localhost:22`
// in a cloudflared config tries `[::1]:22` first; if sshd binds only
// `0.0.0.0:22` (the default on most distros), every SSH-via-tunnel
// attempt fails with `connection refused` on the host side and
// `websocket: bad handshake` on the client side - with zero useful
// signal on either side. ~2 hours of debugging on 2026-05-13.
//
// Fix: name the literal loopback. `127.0.0.1` if IPv4-only listener;
// `[::1]` if IPv6 is verified up. `localhost` is never the right
// answer in a config file.
func NoLocalhostInProxyConfig(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "network/no-localhost-in-proxy-config",
		Category: frames.CategoryNetwork,
	}

	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if proxyLocalhostApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !proxyLocalhostApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > proxyLocalhostMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, proxyLocalhostDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], proxyLocalhostDisableLineMarker) {
				continue
			}
			for _, shape := range proxyLocalhostShapes {
				if shape.Pattern.MatchString(line) {
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, shape.Label))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: shape.Label})
					if len(hits) >= hitCap {
						break filesLoop
					}
					break // one finding per line
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "reverse-proxy configs reference `localhost`: " + strings.Join(hits, "; ")
	res.Fix = "replace `localhost` with the literal loopback IP: `127.0.0.1` for IPv4 listeners or `[::1]` for IPv6. Modern Go resolvers (cloudflared, etc.) return IPv6 first on Linux, so `localhost` connects to `[::1]:port` - which fails when the destination binds only `0.0.0.0:port`."
	return res
}
