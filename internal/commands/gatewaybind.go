// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// defaultUnitPath is where the proxmox setup doc + the shipped
// deploy/gateway/nimblegate-dashboard.service land the systemd unit.
// Overridable via --unit for non-standard installs.
const defaultUnitPath = "/etc/systemd/system/nimblegate-dashboard.service"

// bindAliases maps friendly names to the actual address the daemon binds to.
// Listed in the order the interactive prompt presents them.
var bindAliases = []struct {
	name    string
	addr    string
	caption string
}{
	{"localhost", "127.0.0.1", "loopback only: recommended for public servers behind nginx/SSH tunnel"},
	{"all", "0.0.0.0", "every interface: only safe behind firewall that blocks the dashboard port"},
}

// gatewayBind is the entry for `nimblegate gateway bind [<choice>] [--unit path] [--restart]`.
// Rewrites the systemd unit's ExecStart line so --addr matches the chosen
// bind address, then runs systemctl daemon-reload. Prints the restart
// instruction (doesn't auto-restart - 0.5s downtime should be a deliberate
// operator action).
func gatewayBind(args []string) int {
	return gatewayBindWith(args, os.Stdin, os.Stdout, os.Stderr, realSystemctl)
}

// gatewayBindWith is the testable entry - injects stdin/stdout/stderr for
// interactive-prompt control, and abstracts systemctl so tests don't actually
// run it.
func gatewayBindWith(args []string, stdin io.Reader, stdout, stderr io.Writer, systemctl systemctlRunner) int {
	flags := flag.NewFlagSet("gateway bind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	unit := flags.String("unit", defaultUnitPath, "path to the systemd unit file to rewrite")
	skipReload := flags.Bool("no-reload", false, "don't run systemctl daemon-reload after writing (rare)")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if _, err := os.Stat(*unit); err != nil {
		fmt.Fprintf(stderr, "nimblegate gateway bind: unit file not found at %s\n", *unit)
		fmt.Fprintln(stderr, "  → if this is a fresh install, follow docs/server/SETUP-proxmox-trixie.md")
		fmt.Fprintln(stderr, "  → or pass --unit with the actual path")
		return 1
	}

	// Resolve the chosen address: positional arg, or interactive prompt.
	var choice string
	if flags.NArg() > 0 {
		choice = flags.Arg(0)
	}
	addr, err := resolveBindChoice(choice, stdin, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "nimblegate gateway bind: %v\n", err)
		return 1
	}

	body, err := os.ReadFile(*unit)
	if err != nil {
		fmt.Fprintf(stderr, "nimblegate gateway bind: read unit: %v\n", err)
		return 2
	}
	newBody, prev, changed := rewriteUnitAddr(string(body), addr)
	if !changed {
		fmt.Fprintf(stdout, "nimblegate gateway bind: --addr already set to %s; nothing to do.\n", addr)
		return 0
	}
	if err := os.WriteFile(*unit, []byte(newBody), 0o644); err != nil {
		fmt.Fprintf(stderr, "nimblegate gateway bind: write unit: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "nimblegate gateway bind: --addr %s → %s in %s\n", prev, addr, *unit)

	if !*skipReload {
		if out, err := systemctl("daemon-reload"); err != nil {
			fmt.Fprintf(stderr, "nimblegate gateway bind: systemctl daemon-reload: %v\n%s\n", err, out)
			fmt.Fprintln(stderr, "  → unit file rewrite succeeded; reload manually with `systemctl daemon-reload`")
			return 2
		}
	}
	fmt.Fprintln(stdout, "  → run `systemctl restart nimblegate-dashboard` to apply (0.5s downtime).")
	return 0
}

// resolveBindChoice returns the IP address for a friendly name (localhost/all),
// a literal IP, or - when choice is empty - prompts the operator with a
// numbered list. Validates that any literal value parses as an IP.
func resolveBindChoice(choice string, stdin io.Reader, stdout io.Writer) (string, error) {
	if choice == "" {
		return promptBindChoice(stdin, stdout)
	}
	for _, a := range bindAliases {
		if choice == a.name {
			return a.addr, nil
		}
	}
	// Literal IP - accept v4 or v6.
	if ip := net.ParseIP(choice); ip != nil {
		return ip.String(), nil
	}
	return "", fmt.Errorf("invalid bind choice %q (expected localhost, all, or an IP address)", choice)
}

// promptBindChoice writes a numbered menu to stdout and reads the operator's
// pick from stdin. Three attempts before giving up. Default (empty input) is
// localhost - the safer pick.
func promptBindChoice(stdin io.Reader, stdout io.Writer) (string, error) {
	br := bufio.NewReader(stdin)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprintln(stdout, "Where should the dashboard listen?")
		for i, a := range bindAliases {
			fmt.Fprintf(stdout, "  %d) %s - %s\n", i+1, a.name, a.caption)
		}
		fmt.Fprintf(stdout, "  Or type a literal IP. [Enter for 1=localhost]: ")
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return bindAliases[0].addr, nil
		}
		// Try number first.
		if line == "1" {
			return bindAliases[0].addr, nil
		}
		if line == "2" {
			return bindAliases[1].addr, nil
		}
		if addr, err := resolveBindChoice(line, nil, nil); err == nil {
			return addr, nil
		}
		fmt.Fprintf(stdout, "  invalid choice %q; try again.\n", line)
	}
	return "", fmt.Errorf("no valid choice after 3 attempts")
}

// addrFlagPattern matches --addr<sep><value> in a unit's ExecStart line.
// <sep> is either '=' or whitespace. <value> is any non-whitespace token.
// Capturing groups: 1 = separator (preserved on rewrite), 2 = old value.
var addrFlagPattern = regexp.MustCompile(`--addr([= ])(\S+)`)

// rewriteUnitAddr returns the unit body with --addr updated to newAddr,
// preserving the original separator style (space vs equals). Also returns
// the previous value and whether any change happened (idempotent re-run is
// a no-op). If the ExecStart line has no --addr flag at all, appends one
// using a space separator.
func rewriteUnitAddr(body, newAddr string) (newBody, prevAddr string, changed bool) {
	if m := addrFlagPattern.FindStringSubmatch(body); m != nil {
		prevAddr = m[2]
		if prevAddr == newAddr {
			return body, prevAddr, false
		}
		sep := m[1]
		newBody = addrFlagPattern.ReplaceAllString(body, "--addr"+sep+newAddr)
		return newBody, prevAddr, true
	}
	// No --addr present - try to append to ExecStart=... lines.
	out := &strings.Builder{}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	changedAny := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "ExecStart=") {
			line += " --addr " + newAddr
			changedAny = true
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if !changedAny {
		return body, "", false
	}
	return out.String(), "(absent)", true
}

// systemctlRunner abstracts shelling out to systemctl so tests can inject a
// no-op or canned-error variant.
type systemctlRunner func(args ...string) ([]byte, error)

func realSystemctl(args ...string) ([]byte, error) {
	cmd := exec.Command("systemctl", args...)
	return cmd.CombinedOutput()
}
