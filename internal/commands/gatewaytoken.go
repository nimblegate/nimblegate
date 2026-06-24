// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"nimblegate/internal/auth"
)

// gatewayToken manages agent-API bearer tokens: new <label> | list | revoke <id>.
// Operates on the same _auth.db the dashboard uses.
func gatewayToken(args []string) int {
	fs := flag.NewFlagSet("gateway token", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root")
	authDBPath := fs.String("auth-db", "", "auth DB path (default: <policy-root>/_auth.db)")
	// stdlib flag stops at the first non-flag argument, but flags and
	// positionals arrive in any order (`token new x --policy-root p` or
	// `token --policy-root p new x`) - so re-parse around each positional,
	// collecting them, until only flags remain. The subcommand is the first
	// positional.
	var positional []string
	rem := args
	for {
		_ = fs.Parse(rem)
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rem = fs.Args()[1:]
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway token <new <label>|list|revoke <id>> [--policy-root ...]")
		return 2
	}
	sub, rest := positional[0], positional[1:]

	// Validate subcommand + arity BEFORE opening the store, so usage errors
	// exit 2 without creating an _auth.db as a side effect.
	switch sub {
	case "new", "revoke":
		if len(rest) != 1 {
			fmt.Fprintf(os.Stderr, "usage: nimblegate gateway token %s <%s>\n", sub, map[string]string{"new": "label", "revoke": "id"}[sub])
			return 2
		}
	case "list":
	default:
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway token <new <label>|list|revoke <id>>")
		return 2
	}

	dbPath := *authDBPath
	if dbPath == "" {
		dbPath = filepath.Join(*policyRoot, "_auth.db")
	}
	store, err := auth.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer store.Close()

	switch sub {
	case "new":
		tok, err := store.CreateAPIToken(rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Printf("token (shown once, store it now): %s\n", tok)
		return 0
	case "list":
		list, err := store.ListAPITokens()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if len(list) == 0 {
			fmt.Println("no tokens. Create one: nimblegate gateway token new <label>")
			return 0
		}
		for _, t := range list {
			state := "active"
			if t.RevokedAt != 0 {
				state = "revoked"
			}
			fmt.Printf("%d\t%s\t%s\n", t.ID, t.Label, state)
		}
		return 0
	case "revoke":
		id, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: id must be a number")
			return 2
		}
		if err := store.RevokeAPIToken(id); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("revoked")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway token <new <label>|list|revoke <id>>")
		return 2
	}
}
