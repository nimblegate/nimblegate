// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"nimblegate/internal/gateway"
)

// gatewayGitShell is the forced-command entrypoint for scoped SSH access. The
// authorized_keys line is `command="nimblegate gateway shell --key <fp> …"`, so
// sshd runs THIS instead of the client's command and passes the real one in
// $SSH_ORIGINAL_COMMAND. We authorize (parse + symlink-safe resolve + per-key
// ACL) and only then exec the git transfer command against the resolved bare
// repo. A denied or malformed request never reaches git. (Distinct from the
// dashboard "gwshell" web UI in gatewayshell.go.)
func gatewayGitShell(args []string) int {
	fs := flag.NewFlagSet("gateway shell", flag.ExitOnError)
	key := fs.String("key", "", "fingerprint of the connecting key (set by the authorized_keys forced command)")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "bare-repo root")
	scoped := fs.Bool("scoped", false, "enforce the per-key repo ACL (set by the forced command only in scoped mode; absent = single-tenant, any authorized key reaches any repo)")
	_ = fs.Parse(args)

	orig := os.Getenv("SSH_ORIGINAL_COMMAND")
	subverb, bareDir, err := gateway.AuthorizeShellRequest(orig, *key, *reposRoot, *policyRoot, *scoped)
	if err != nil {
		// Camouflage: the client sees a vanilla "not found" - a normal host
		// doesn't distinguish missing from forbidden, and never reveals it's a
		// gateway. The real reason (parse / resolve / ACL deny) goes to the
		// operator audit, where probing is exactly what you want to see.
		_ = gateway.AppendEvent(*policyRoot, gateway.Event{Event: "ssh-access-denied", OK: false,
			Payload: map[string]any{"key": *key, "command": orig, "reason": err.Error()}})
		fmt.Fprintln(os.Stderr, "ERROR: Repository not found.")
		return 128
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		_ = gateway.AppendEvent(*policyRoot, gateway.Event{Event: "shell-git-missing", OK: false,
			Payload: map[string]any{"error": err.Error()}})
		fmt.Fprintln(os.Stderr, "ERROR: Repository not found.")
		return 128
	}
	// git <subverb> <bareDir>, SSH channel stdio passed straight through (the
	// pack protocol streams over it). Propagate git's exit code.
	cmd := exec.Command(gitBin, subverb, bareDir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		_ = gateway.AppendEvent(*policyRoot, gateway.Event{Event: "shell-exec-failed", OK: false,
			Payload: map[string]any{"error": err.Error()}})
		fmt.Fprintln(os.Stderr, "ERROR: Could not read from remote repository.")
		return 128
	}
	return 0
}
