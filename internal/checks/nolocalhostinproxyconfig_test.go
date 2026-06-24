// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeConf(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNoLocalhostInProxyConfig_BlocksCloudflared(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "etc/cloudflared/config.yml", `tunnel: abcd
credentials-file: /etc/cloudflared/creds.json
ingress:
  - hostname: ssh.example.com
    service: ssh://localhost:22
  - service: http_status:404
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "cloudflared service") {
		t.Errorf("expected cloudflared shape label; got: %s", got.Reason)
	}
}

func TestNoLocalhostInProxyConfig_PassesWith127(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "etc/cloudflared/config.yml", `ingress:
  - hostname: ssh.example.com
    service: ssh://127.0.0.1:22
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoLocalhostInProxyConfig_BlocksNginx(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "nginx.conf", `server {
  listen 80;
  location / {
    proxy_pass http://localhost:8080;
  }
}
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoLocalhostInProxyConfig_BlocksNginxUpstream(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "nginx.conf", `upstream backend {
  server localhost:3000;
  server localhost:3001;
}
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoLocalhostInProxyConfig_BlocksCaddy(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "Caddyfile", `example.com {
    reverse_proxy localhost:8080
}
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoLocalhostInProxyConfig_NonProxyFileIgnored(t *testing.T) {
	root := t.TempDir()
	// A README mentioning localhost should NOT trip the check.
	writeConf(t, root, "README.md", `Run with: curl http://localhost:8080`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS - markdown is not in the proxy-config applies-to set", got.Outcome)
	}
}

func TestNoLocalhostInProxyConfig_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeConf(t, root, "etc/cloudflared/config.yml", `ingress:
  # appframes:disable-next-line network/no-localhost-in-proxy-config
  - service: http://localhost:9999  # example only
  - service: ssh://localhost:22     # this one should still fire
`)
	got := NoLocalhostInProxyConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (ssh line should still fire)\nreason: %s", got.Outcome, got.Reason)
	}
	// Exactly one Hit should survive - the line-disable suppressed the http
	// case; only the ssh line on line 4 should be present.
	if len(got.Hits) != 1 {
		t.Fatalf("got %d hits; want 1 (line disable should hide line 3)\nhits: %+v", len(got.Hits), got.Hits)
	}
	if got.Hits[0].Line != 4 {
		t.Errorf("surviving hit is on line %d; want 4 (the ssh://localhost:22 line)", got.Hits[0].Line)
	}
}
