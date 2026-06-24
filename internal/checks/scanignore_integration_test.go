// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/scanignore"
)

// TestNoPrivateKeys_RespectsAppframesIgnoreMarker verifies that a directory
// flagged via .appframes-ignore is skipped by an end-to-end file-scanning
// check. The check would normally BLOCK on a PEM private key - the marker
// should cause it to PASS.
func TestNoPrivateKeys_RespectsAppframesIgnoreMarker(t *testing.T) {
	root := t.TempDir()
	// A "served downloads" dir with a marker that says "ignore everything here."
	served := filepath.Join(root, "public", "downloads")
	if err := os.MkdirAll(served, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(served, ".appframes-ignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A PEM block that would normally BLOCK.
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKC...\n-----END RSA PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(served, "sample.pem"), []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}

	matcher, err := scanignore.New(root, DefaultExcludes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
		IgnorePath:  matcher.Match,
	}
	got := NoPrivateKeysInRepo(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (the .appframes-ignore marker should hide the served-content PEM)\nreason: %s", got.Outcome, got.Reason)
	}
}

// TestNoPrivateKeys_RespectsExcludePathsGlob verifies that a path-glob from
// [scan].exclude-paths suppresses an otherwise BLOCK-worthy file.
func TestNoPrivateKeys_RespectsExcludePathsGlob(t *testing.T) {
	root := t.TempDir()
	served := filepath.Join(root, "public", "downloads")
	if err := os.MkdirAll(served, 0o755); err != nil {
		t.Fatal(err)
	}
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(served, "sample.pem"), []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}
	// A copy elsewhere - should still BLOCK because the glob is specific.
	otherDir := filepath.Join(root, "src")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "real-leak.pem"), []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}

	matcher, err := scanignore.New(root, DefaultExcludes(), []string{"public/downloads/**"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
		IgnorePath:  matcher.Match,
	}
	got := NoPrivateKeysInRepo(ctx)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (the src/ leak is NOT covered by the glob)\nreason: %s", got.Outcome, got.Reason)
	}
	// The blocked hit should be from src/, not public/downloads/.
	for _, h := range got.Hits {
		if filepath.Base(filepath.Dir(h.File)) == "downloads" {
			t.Errorf("a hit from public/downloads/ leaked through: %s", h.File)
		}
	}
}
