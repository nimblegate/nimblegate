// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// policyRootNotice is the loud-config-validation classifier: it must distinguish
// a misconfigured/empty policy root (warn) from a healthy one (silent), so a
// wrong --policy-root can't masquerade as a normal fresh gateway.
func TestPolicyRootNotice(t *testing.T) {
	t.Run("missing dir warns and names the path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		got := policyRootNotice(missing)
		if got == "" || !strings.Contains(got, missing) {
			t.Fatalf("missing dir: want a warning naming %q, got %q", missing, got)
		}
	})

	t.Run("dir with zero registered repos warns", func(t *testing.T) {
		empty := t.TempDir() // exists, no */gateway.toml
		got := policyRootNotice(empty)
		if got == "" || !strings.Contains(got, "registered") {
			t.Fatalf("empty root: want a 'no repos registered' warning, got %q", got)
		}
	})

	t.Run("dir with a registered repo is silent", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "myrepo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "myrepo", "gateway.toml"), []byte("upstream = \"x\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := policyRootNotice(root); got != "" {
			t.Fatalf("registered repo present: want silent, got %q", got)
		}
	})
}

// The notice must surface in the page the operator looks at, not just stderr.
func TestDashboardPageRendersConfigBanner(t *testing.T) {
	notice := "No repos registered under /wrong/root"
	rec := httptest.NewRecorder()
	renderGwPage(rec, gateway.ViewModel{}, notice, chromeData{})
	body := rec.Body.String()
	if !strings.Contains(body, `class="warn"`) || !strings.Contains(body, notice) {
		t.Fatalf("dashboard page missing config banner; body:\n%s", body)
	}

	// And it must be absent when the root is healthy (empty notice).
	rec = httptest.NewRecorder()
	renderGwPage(rec, gateway.ViewModel{}, "", chromeData{})
	if strings.Contains(rec.Body.String(), `class="warn"`) {
		t.Fatalf("dashboard page should not render a banner when notice is empty")
	}
}
