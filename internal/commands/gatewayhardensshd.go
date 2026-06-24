// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// sshdHardeningConf mirrors deploy/gateway/sshd-hardening.conf. Kept here so the
// command is self-contained (the binary can write it on any gateway). Disables
// password auth (brute-force surface) and, for the git user, all forwarding +
// pty (so an authorized key can ONLY run git over git-shell, never tunnel).
const sshdHardeningConf = `# nimblegate gateway: sshd hardening (written by 'nimblegate gateway harden-sshd').
#
# ⚠ KEEP YOUR CURRENT SSH SESSION OPEN. Confirm a NEW key-based login works in a
#   separate terminal BEFORE closing this one. A bad config can lock you out.

# Key-only authentication. Ensure root/admin access is key-based first.
PasswordAuthentication no
KbdInteractiveAuthentication no

# The git user receives pushes via git-shell only: no pty, no forwarding, so a
# key cannot be used to tunnel through the gateway. (Dashboard-added keys also
# carry 'restrict'; this is defence in depth.)
Match User git
    AllowTcpForwarding no
    AllowAgentForwarding no
    AllowStreamLocalForwarding no
    X11Forwarding no
    PermitTTY no
    PermitTunnel no
`

// gatewayHardenSSHD writes the sshd hardening drop-in and validates it. It does
// NOT reload sshd - that's the operator's deliberate step, after confirming a
// new session works (lock-out safety). Dry-run by default.
func gatewayHardenSSHD(args []string) int {
	fs := flag.NewFlagSet("gateway harden-sshd", flag.ExitOnError)
	dir := fs.String("sshd-config-dir", "/etc/ssh/sshd_config.d", "sshd drop-in config directory")
	apply := fs.Bool("apply", false, "write the config (default: dry-run, print only)")
	_ = fs.Parse(args)
	target := filepath.Join(*dir, "nimblegate-git.conf")

	if !*apply {
		fmt.Print(sshdHardeningConf)
		fmt.Fprintf(os.Stderr, "\nDRY RUN. Nothing written. Re-run with --apply to write %s\n", target)
		return 0
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "harden-sshd: %v\n", err)
		return 1
	}
	if err := os.WriteFile(target, []byte(sshdHardeningConf), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "harden-sshd: %v\n", err)
		return 1
	}
	// Validate only when writing to the real drop-in dir (sshd -t reads the
	// active config; a custom dir isn't included, so validating it is moot).
	if *dir == "/etc/ssh/sshd_config.d" {
		if _, err := exec.LookPath("sshd"); err == nil {
			if out, err := exec.Command("sshd", "-t").CombinedOutput(); err != nil {
				_ = os.Remove(target)
				fmt.Fprintf(os.Stderr, "harden-sshd: sshd -t failed; reverted %s:\n%s\n", target, out)
				return 1
			}
			fmt.Printf("wrote %s: sshd -t OK\n", target)
		} else {
			fmt.Printf("wrote %s (sshd not found; run 'sshd -t' before reload)\n", target)
		}
	} else {
		fmt.Printf("wrote %s (custom dir; ensure it's included by sshd_config, then 'sshd -t')\n", target)
	}
	fmt.Fprintln(os.Stderr, "IMPORTANT: keep this SSH session open; confirm a NEW key login works in another terminal, THEN: systemctl reload ssh")
	return 0
}
