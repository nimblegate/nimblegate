// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/gateway"
)

func TestGatewayAccess_grantThenRevoke(t *testing.T) {
	policyRoot := t.TempDir()
	acl := gateway.AccessStore{PolicyRoot: policyRoot}
	const fp = "SHA256:abc"

	if code := gatewayAccess([]string{"grant", "--repo", "demo", "--key", fp, "--policy-root", policyRoot}); code != 0 {
		t.Fatalf("grant returned %d", code)
	}
	if ok, _ := acl.Allows("demo", fp, true); !ok {
		t.Error("grant (default write) should allow push")
	}

	if code := gatewayAccess([]string{"revoke", "--repo", "demo", "--key", fp, "--policy-root", policyRoot}); code != 0 {
		t.Fatalf("revoke returned %d", code)
	}
	if ok, _ := acl.Allows("demo", fp, false); ok {
		t.Error("revoke should deny")
	}
}

func TestGatewayAccess_grantReadOnly(t *testing.T) {
	policyRoot := t.TempDir()
	if code := gatewayAccess([]string{"grant", "--repo", "demo", "--key", "SHA256:ro", "--read", "--policy-root", policyRoot}); code != 0 {
		t.Fatalf("grant --read returned %d", code)
	}
	acl := gateway.AccessStore{PolicyRoot: policyRoot}
	if ok, _ := acl.Allows("demo", "SHA256:ro", false); !ok {
		t.Error("read grant should allow fetch")
	}
	if ok, _ := acl.Allows("demo", "SHA256:ro", true); ok {
		t.Error("read grant must not allow push")
	}
}

func TestGatewayAccess_requiresArgs(t *testing.T) {
	if gatewayAccess(nil) == 0 {
		t.Error("no action should be a usage error")
	}
	if gatewayAccess([]string{"bogus"}) == 0 {
		t.Error("unknown action should error")
	}
	if gatewayAccess([]string{"grant", "--policy-root", t.TempDir()}) == 0 {
		t.Error("grant without --repo/--key should error")
	}
}
