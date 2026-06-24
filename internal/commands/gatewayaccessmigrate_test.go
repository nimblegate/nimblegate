// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// Enabling scoped access must rewrite EVERY existing key to a forced command
// (else a plain key bypasses scoping) and seed the ACL so nothing breaks: every
// existing key keeps access to every registered repo, which operators then
// tighten.
func TestMigrateToScopedAccess(t *testing.T) {
	dir := t.TempDir()
	policyRoot := filepath.Join(dir, "cfg")
	for _, r := range []string{"alpha", "beta"} {
		if err := (gateway.FilePolicyStore{Root: policyRoot}).Save(gateway.Policy{Repo: r, UpstreamURL: "file:///u"}); err != nil {
			t.Fatal(err)
		}
	}
	keysPath := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(keysPath, []byte(makePubkey(t, "alice")+"\n"+makePubkey(t, "bob")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	keysN, grantsN, err := migrateToScopedAccess(keysPath, "/usr/local/bin/nimblegate", policyRoot, filepath.Join(dir, "repos"))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if keysN != 2 {
		t.Errorf("keysN = %d, want 2", keysN)
	}
	if grantsN != 4 {
		t.Errorf("grantsN = %d, want 4 (2 keys × 2 repos)", grantsN)
	}

	b, _ := os.ReadFile(keysPath)
	if c := strings.Count(string(b), "gateway shell --key SHA256:"); c != 2 {
		t.Errorf("expected 2 forced-command lines, got %d:\n%s", c, b)
	}
	acl := gateway.AccessStore{PolicyRoot: policyRoot}
	for _, r := range []string{"alpha", "beta"} {
		al, _ := acl.Load(r)
		if len(al.Grants) != 2 {
			t.Errorf("repo %s: %d grants, want 2", r, len(al.Grants))
		}
	}
}
