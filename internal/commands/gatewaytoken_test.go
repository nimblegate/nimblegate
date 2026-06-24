// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"path/filepath"
	"testing"

	"nimblegate/internal/auth"
)

func TestGatewayTokenLifecycleViaCLI(t *testing.T) {
	root := t.TempDir()
	if code := gatewayToken([]string{"new", "agent-1", "--policy-root", root}); code != 0 {
		t.Fatalf("new: exit %d", code)
	}
	if code := gatewayToken([]string{"list", "--policy-root", root}); code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	if code := gatewayToken([]string{"revoke", "1", "--policy-root", root}); code != 0 {
		t.Fatalf("revoke: exit %d", code)
	}
	store, err := auth.Open(filepath.Join(root, "_auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	list, err := store.ListAPITokens()
	if err != nil || len(list) != 1 || list[0].RevokedAt == 0 {
		t.Fatalf("token not revoked: %+v %v", list, err)
	}
	if code := gatewayToken([]string{}); code != 2 {
		t.Errorf("no args must exit 2, got %d", code)
	}
}

func TestGatewayTokenFlagsFirstOrdering(t *testing.T) {
	root := t.TempDir()
	if code := gatewayToken([]string{"--policy-root", root, "new", "agent-2"}); code != 0 {
		t.Fatalf("flags-first ordering: exit %d", code)
	}
	if code := gatewayToken([]string{"bogus", "--policy-root", root}); code != 2 {
		t.Errorf("unknown subcommand must exit 2, got %d", code)
	}
	if code := gatewayToken([]string{"new", "--policy-root", root}); code != 2 {
		t.Errorf("missing label must exit 2, got %d", code)
	}
}
