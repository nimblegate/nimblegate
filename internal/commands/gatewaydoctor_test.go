// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHealthDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	seedActiveRepo(t, policyRoot, reposRoot, "demo")

	// Config-only view (online=false): renders without running live checks and
	// offers the opt-in link rather than dialing the SSH gate or the upstream.
	html := string(renderHealthDiagnostics(policyRoot, reposRoot, "", "127.0.0.1:7900", false))
	if !strings.Contains(html, "Diagnostics") {
		t.Fatalf("diagnostics body missing header: %s", html)
	}
	if !strings.Contains(html, `class="gw-doc-fail"`) {
		t.Fatalf("expected a FAIL line (seed repo has no gated refs): %s", html)
	}
	if !strings.Contains(html, "Connect a dev machine") {
		t.Fatalf("expected a connect block: %s", html)
	}
	if !strings.Contains(html, "online=1") {
		t.Fatalf("config-only view should offer the live-checks opt-in link: %s", html)
	}

	strip := healthTabStrip("diagnostics")
	if !strings.Contains(strip, `autopr-tab active">Diagnostics`) {
		t.Fatalf("diagnostics tab not marked active: %s", strip)
	}
}
