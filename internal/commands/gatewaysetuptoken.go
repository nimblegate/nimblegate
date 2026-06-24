// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"

	"nimblegate/internal/auth"
)

// gatewaySetupToken prints the pending dashboard setup token - the one-time
// token you paste at /setup to claim the admin login. It's the bare-metal
// equivalent of reading the token out of the container logs (`docker logs
// <container> | grep "setup token"`): read-only, it shows whatever the
// dashboard wrote to <policy-root>/_setup_token on first start, never
// generating or consuming one.
//
// The token goes to stdout (so it pipes/copies cleanly); the usage hint goes to
// stderr. Exit 1 when there's nothing to claim - the admin is already set up,
// or the dashboard hasn't started yet.
func gatewaySetupToken(args []string) int {
	fs := flag.NewFlagSet("gateway setup-token", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root (the dashboard's --policy-root, e.g. /srv/gateway/cfg)")
	_ = fs.Parse(args)

	tok, present, err := auth.ReadSetupToken(*policyRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway setup-token: %v\n", err)
		return 2
	}
	if !present {
		fmt.Fprintln(os.Stderr, "No pending setup token. Either the admin login is already claimed,")
		fmt.Fprintln(os.Stderr, "or the dashboard hasn't started yet (it writes the token on first start).")
		fmt.Fprintln(os.Stderr, "To reset a lost admin login, see docs/server/README.md § Recover.")
		return 1
	}
	fmt.Println(tok)
	fmt.Fprintln(os.Stderr, "Paste this at http://<gateway>:7900/setup to claim the admin login.")
	return 0
}
