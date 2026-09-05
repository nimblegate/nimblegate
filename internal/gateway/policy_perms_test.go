// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFilePolicyStore_Save_WritesMode0640 confirms gateway.toml keeps world
// read off ([notification.webhook] secrets) while staying readable to the
// gateway group - the privilege-separated relay runs as a different user and
// reads the upstream URL from this file, and 0600 locked it out of every repo.
func TestFilePolicyStore_Save_WritesMode0640(t *testing.T) {
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
	if mode != 0o640 {
		t.Errorf("gateway.toml mode = %o, want 640 (0600 locks the relay user out)", mode)
	}
}

// TestFilePolicyStore_Save_TightensExistingLoosePerms confirms a pre-existing
// gateway.toml at 0644 (written by an older binary that used os.Create's
// default mode) has world read removed on the next Save - matches the cred.go
// pattern that enforces the mode even on pre-existing files.
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
	if mode != 0o640 {
		t.Errorf("after re-Save, gateway.toml mode = %o, want 640 (Chmod enforce-on-existing missing?)", mode)
	}
}

// The relay service runs as its own user and creates relay-status.json inside
// the repo's policy dir, which the dashboard created. Without group write it
// cannot; without setgid, what it does write leaves the shared group and the
// dashboard can no longer read it back.
func TestAddRepo_PolicyDirIsGroupWritableAndSetgid(t *testing.T) {
	policyRoot, reposRoot := t.TempDir(), t.TempDir()
	if err := AddRepo(AddOptions{
		Name:        "demo",
		UpstreamURL: "file:///tmp/up.git",
		Enabled:     true,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     "/usr/local/bin/nimblegate",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	st, err := os.Stat(filepath.Join(policyRoot, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSetgid == 0 {
		t.Errorf("policy dir mode = %v, want setgid so relay-written files keep the gateway group", st.Mode())
	}
	if st.Mode().Perm()&0o020 == 0 {
		t.Errorf("policy dir perm = %o, want group write so the relay can write relay-status.json", st.Mode().Perm())
	}
}
