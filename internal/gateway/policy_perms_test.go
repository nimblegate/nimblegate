// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFilePolicyStore_Save_WritesMode0600 confirms gateway.toml is written
// with restrictive perms so [notification.webhook] secrets aren't world-
// readable when the file lands under the policy root.
func TestFilePolicyStore_Save_WritesMode0600(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	p := Policy{
		Repo:        "demo",
		UpstreamURL: "https://example.com/demo.git",
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := os.Stat(filepath.Join(root, "demo", "gateway.toml"))
	if err != nil {
		t.Fatal(err)
	}
	mode := st.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("gateway.toml mode = %o, want 600", mode)
	}
}

// TestFilePolicyStore_Save_TightensExistingLoosePerms confirms a pre-existing
// gateway.toml at 0644 (written by an older binary that used os.Create's
// default mode) gets re-tightened to 0600 on the next Save - matches the
// cred.go pattern that enforces 0600 even on pre-existing files.
func TestFilePolicyStore_Save_TightensExistingLoosePerms(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gateway.toml")
	// Simulate an old-binary-written file at 0644.
	if err := os.WriteFile(path, []byte("# stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	s := FilePolicyStore{Root: root}
	p := Policy{
		Repo:        "demo",
		UpstreamURL: "https://example.com/demo.git",
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := st.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("after re-Save, gateway.toml mode = %o, want 600 (Chmod enforce-on-existing missing?)", mode)
	}
}
