// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// unscopedKeys returns a label for every authorized_keys entry that is NOT
// routed through the scoped-access forced command - i.e. a plain or
// restrict-only key that would reach git-shell directly and therefore BYPASS
// per-repo scoping. A missing file is not an error (no keys to bypass).
func unscopedKeys(keysPath string) ([]string, error) {
	data, err := os.ReadFile(keysPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var bad []string
	for rest := data; len(bytes.TrimSpace(rest)) > 0; {
		pk, comment, options, rem, perr := ssh.ParseAuthorizedKey(rest)
		if perr != nil {
			if idx := bytes.IndexByte(rest, '\n'); idx >= 0 {
				rest = rest[idx+1:]
				continue
			}
			break
		}
		rest = rem
		scoped := false
		for _, o := range options {
			if strings.Contains(o, "gateway shell --key") {
				scoped = true
				break
			}
		}
		if !scoped {
			label := ssh.FingerprintSHA256(pk)
			if comment != "" {
				label += " (" + comment + ")"
			}
			bad = append(bad, label)
		}
	}
	return bad, nil
}

// scopedAccessGuard fails closed: if scoped access is requested but any key in
// keysPath would bypass it (not a forced-command key), it returns an error so
// the caller refuses to start. This is what stops scoped mode from being
// enabled while leaving pre-existing plain keys silently unscoped - the
// migrate footgun. Run `gateway access migrate` to fix.
func scopedAccessGuard(scoped bool, keysPath string) error {
	if !scoped || keysPath == "" {
		return nil
	}
	bad, err := unscopedKeys(keysPath)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", keysPath, err)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d key(s) would BYPASS per-repo scoping (run 'nimblegate gateway access migrate' first): %s",
			len(bad), strings.Join(bad, ", "))
	}
	return nil
}
