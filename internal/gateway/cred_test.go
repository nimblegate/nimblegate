// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCredentialStore(t *testing.T) {
	root := t.TempDir()
	// No manual MkdirAll: Save must create the directory itself.
	s := FileCredentialStore{Root: root}
	if err := s.Save("demo", "ghp_secrettoken"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "demo", "credential"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("credential perms = %o, want 640", info.Mode().Perm())
	}
	got, err := s.Load("demo")
	if err != nil || got != "ghp_secrettoken" {
		t.Errorf("Load = %q, %v", got, err)
	}
	if got, err := s.Load("noauth"); err != nil || got != "" {
		t.Errorf("missing cred should be empty+nil, got %q, %v", got, err)
	}
}

func TestFileCredentialStore_SaveEnforces0640OnExistingFile(t *testing.T) {
	root := t.TempDir()
	repo := "tighten"
	credFile := filepath.Join(root, repo, "credential")

	// Pre-create the file with loose 0644 permissions.
	if err := os.MkdirAll(filepath.Join(root, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := FileCredentialStore{Root: root}
	if err := s.Save(repo, "x"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(credFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("perms after Save on pre-existing file = %o, want 0640", info.Mode().Perm())
	}
}

// The credential store builds its path from the repo name the same way the
// policy store does, and had no guard at all - on the more sensitive file.
func TestFileCredentialStore_rejectsNamesThatEscapeTheRoot(t *testing.T) {
	root := t.TempDir()
	s := FileCredentialStore{Root: root}
	for _, name := range []string{"../escape", "a/b", ".."} {
		if err := s.Save(name, "ghp_secret"); err == nil {
			t.Errorf("Save(%q) succeeded; want rejection", name)
		}
		if _, err := s.Load(name); err == nil {
			t.Errorf("Load(%q) succeeded; want rejection", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape")); err == nil {
		t.Error("a rejected name still wrote outside the root")
	}
}
