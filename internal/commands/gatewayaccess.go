// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"

	"nimblegate/internal/gateway"
)

// gatewayAccess dispatches `nimblegate gateway access <grant|revoke|list|migrate>`
// - the operator-facing management of per-key repo ACLs (the CLI half; the
// dashboard is the GUI half).
func gatewayAccess(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway access <grant|revoke|list|migrate> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "grant":
		return gatewayAccessGrant(rest)
	case "revoke":
		return gatewayAccessRevoke(rest)
	case "list":
		return gatewayAccessList(rest)
	case "migrate":
		return gatewayAccessMigrate(rest)
	default:
		fmt.Fprintf(os.Stderr, "gateway access: unknown action %q (want grant|revoke|list|migrate)\n", sub)
		return 2
	}
}

func gatewayAccessGrant(args []string) int {
	fs := flag.NewFlagSet("gateway access grant", flag.ExitOnError)
	repo := fs.String("repo", "", "repo name")
	key := fs.String("key", "", "key fingerprint (SHA256:…)")
	readOnly := fs.Bool("read", false, "grant fetch-only (default: push+fetch)")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	_ = fs.Parse(args)
	if *repo == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "gateway access grant: --repo and --key are required")
		return 2
	}
	access := "write"
	if *readOnly {
		access = "read"
	}
	if err := (gateway.AccessStore{PolicyRoot: *policyRoot}).Grant(*repo, *key, access, ""); err != nil {
		fmt.Fprintf(os.Stderr, "gateway access grant: %v\n", err)
		return 1
	}
	fmt.Printf("granted %s %s on %s\n", *key, access, *repo)
	return 0
}

func gatewayAccessRevoke(args []string) int {
	fs := flag.NewFlagSet("gateway access revoke", flag.ExitOnError)
	repo := fs.String("repo", "", "repo name")
	key := fs.String("key", "", "key fingerprint (SHA256:…)")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	_ = fs.Parse(args)
	if *repo == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "gateway access revoke: --repo and --key are required")
		return 2
	}
	if err := (gateway.AccessStore{PolicyRoot: *policyRoot}).Revoke(*repo, *key); err != nil {
		fmt.Fprintf(os.Stderr, "gateway access revoke: %v\n", err)
		return 1
	}
	fmt.Printf("revoked %s on %s\n", *key, *repo)
	return 0
}

func gatewayAccessList(args []string) int {
	fs := flag.NewFlagSet("gateway access list", flag.ExitOnError)
	repo := fs.String("repo", "", "repo name")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	_ = fs.Parse(args)
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "gateway access list: --repo is required")
		return 2
	}
	al, err := (gateway.AccessStore{PolicyRoot: *policyRoot}).Load(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway access list: %v\n", err)
		return 1
	}
	if len(al.Grants) == 0 {
		fmt.Printf("no grants on %s (no key may reach it under scoped access)\n", *repo)
		return 0
	}
	for _, g := range al.Grants {
		fmt.Printf("%s\t%s\t%s\n", g.Fingerprint, g.Access, g.Comment)
	}
	return 0
}
