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
	html := string(renderHealthDiagnostics(policyRoot, reposRoot, "", "127.0.0.1:7900", false, ""))
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

	strip := healthTabStrip("diagnostics", "")
	if !strings.Contains(strip, `autopr-tab active">Diagnostics`) {
		t.Fatalf("diagnostics tab not marked active: %s", strip)
	}
}

// With a repo selected in the top bar, Diagnostics lists only that repo's
// checks; gateway-wide checks stay.
func TestRenderHealthDiagnostics_RepoSelection(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	for _, d := range []string{policyRoot, reposRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seedActiveRepo(t, policyRoot, reposRoot, "alpha-repo")
	seedActiveRepo(t, policyRoot, reposRoot, "beta-repo")

	all := string(renderHealthDiagnostics(policyRoot, reposRoot, "", "127.0.0.1:7900", false, ""))
	one := string(renderHealthDiagnostics(policyRoot, reposRoot, "", "127.0.0.1:7900", false, "beta-repo"))
	if !strings.Contains(all, "alpha-repo") || !strings.Contains(all, "beta-repo") {
		t.Fatal("unfiltered view should list both repos")
	}
	if strings.Contains(one, "alpha-repo") || !strings.Contains(one, "beta-repo") {
		t.Errorf("filtered view should list only beta-repo")
	}
	if !strings.Contains(one, "online=1&amp;repo=beta-repo") {
		t.Errorf("live-checks link should keep the selection")
	}
	if strip := healthTabStrip("diagnostics", "beta-repo"); !strings.Contains(strip, `/health?tab=status&amp;repo=beta-repo`) {
		t.Errorf("tab strip should keep the selection: %s", strip)
	}
}
