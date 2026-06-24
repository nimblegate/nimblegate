// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"nimblegate/internal/gateway"
)

// migrateToScopedAccess prepares an existing gateway for --scoped-access: it
// rewrites EVERY key in keysPath to a forced-command line (so no plain key
// bypasses the scoping) and grants each key write access to every registered
// repo - preserving the pre-scoping any-key-any-repo behavior as the starting
// ACL, which operators then tighten per repo. Returns (keys, grants) counts.
// Re-runnable: grants are idempotent and the rewrite is canonical.
func migrateToScopedAccess(keysPath, exe, policyRoot, reposRoot string) (keysN, grantsN int, err error) {
	data, err := os.ReadFile(keysPath)
	if err != nil {
		return 0, 0, fmt.Errorf("read authorized_keys: %w", err)
	}
	repos := listGatewayRepos(policyRoot)
	acl := gateway.AccessStore{PolicyRoot: policyRoot}

	var lines []string
	for rest := data; len(bytes.TrimSpace(rest)) > 0; {
		pk, comment, _, rem, perr := ssh.ParseAuthorizedKey(rest)
		if perr != nil { // skip a malformed line, advance to next
			if idx := bytes.IndexByte(rest, '\n'); idx >= 0 {
				rest = rest[idx+1:]
				continue
			}
			break
		}
		rest = rem
		fp := ssh.FingerprintSHA256(pk)
		canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))
		if comment != "" {
			canonical += " " + comment
		}
		lines = append(lines, forcedCommandLine(exe, policyRoot, reposRoot, fp, canonical, true))
		keysN++
		for _, repo := range repos {
			if gerr := acl.Grant(repo, fp, "write", comment); gerr != nil {
				return 0, 0, fmt.Errorf("grant %s on %s: %w", fp, repo, gerr)
			}
			grantsN++
		}
	}
	if err := rewriteKeysFile(keysPath, lines); err != nil {
		return 0, 0, err
	}
	return keysN, grantsN, nil
}

// rewriteKeysFile atomically replaces keysPath with the given lines (temp file
// + rename, so a crash can't truncate the live authorized_keys → lockout).
func rewriteKeysFile(keysPath string, lines []string) error {
	tmp, err := os.CreateTemp(filepath.Dir(keysPath), ".authorized_keys.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), keysPath)
}

// gatewayAccessMigrate is `nimblegate gateway access-migrate`: the one-time
// step to enable scoped access on a gateway with existing keys.
func gatewayAccessMigrate(args []string) int {
	fs := flag.NewFlagSet("gateway access-migrate", flag.ExitOnError)
	keysPath := fs.String("ssh-authorized-keys", "/home/git/.ssh/authorized_keys", "authorized_keys file to rewrite")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "bare-repo root")
	_ = fs.Parse(args)
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway access-migrate: %v\n", err)
		return 1
	}
	keysN, grantsN, err := migrateToScopedAccess(*keysPath, exe, *policyRoot, *reposRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway access-migrate: %v\n", err)
		return 1
	}
	fmt.Printf("scoped access migrated: %d key(s) rewritten with forced commands, %d grant(s) seeded (each key → every repo).\n", keysN, grantsN)
	fmt.Fprintln(os.Stderr, "Now run the dashboard with --scoped-access, and tighten per-repo grants. Keep a session open and test a new push before relying on it.")
	return 0
}
