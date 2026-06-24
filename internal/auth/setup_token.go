// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SetupToken is a single-use bootstrap token used to claim the initial admin
// account. Generated once on first start (when no users exist) and printed to
// stdout + persisted at <policyRoot>/_setup_token (mode 0600). Consumed on
// successful POST /setup; the file is deleted at that point.
//
// Format: 16 hex chars (64 bits of entropy) split into four 4-char groups by
// dashes: XXXX-XXXX-XXXX-XXXX. 64 bits is single-use-friendly: brute-forcing
// it online at 1000 req/sec would take ~580 million years.

const setupTokenFile = "_setup_token"

// SetupTokenPath returns the canonical path under policyRoot.
func SetupTokenPath(policyRoot string) string {
	return filepath.Join(policyRoot, setupTokenFile)
}

// EnsureSetupToken reads (or creates) the setup token file. Returns the
// human-formatted token (XXXX-XXXX-XXXX-XXXX) and `fresh` true if this call
// generated it. Caller decides whether to print to stdout.
func EnsureSetupToken(policyRoot string) (token string, fresh bool, err error) {
	if policyRoot == "" {
		return "", false, errors.New("policyRoot required")
	}
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		return "", false, err
	}
	path := SetupTokenPath(policyRoot)
	if existing, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(existing))
		if formatted, ok := validateAndFormat(s); ok {
			return formatted, false, nil
		}
		// File present but unreadable as a token - treat as missing and
		// regenerate (operator might have edited it).
	}
	var raw [8]byte // 64 bits
	if _, err := rand.Read(raw[:]); err != nil {
		return "", false, err
	}
	hexStr := hex.EncodeToString(raw[:])
	if err := os.WriteFile(path, []byte(hexStr+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return formatHex(hexStr), true, nil
}

// ReadSetupToken returns the pending one-time setup token (formatted as the
// /setup page expects) WITHOUT generating or consuming one. present is false
// when there's no token to claim - either the admin has already been claimed
// (the token is one-shot, deleted on consume) or the dashboard hasn't started
// yet (it writes the token on first start). This is the read-only retrieval
// behind `nimblegate gateway setup-token` - the bare-metal equivalent of
// reading the token out of the container logs.
func ReadSetupToken(policyRoot string) (token string, present bool, err error) {
	if policyRoot == "" {
		return "", false, errors.New("policyRoot required")
	}
	data, err := os.ReadFile(SetupTokenPath(policyRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	formatted, ok := validateAndFormat(strings.TrimSpace(string(data)))
	if !ok {
		return "", false, fmt.Errorf("setup token file %s is malformed", SetupTokenPath(policyRoot))
	}
	return formatted, true, nil
}

// ConsumeSetupToken verifies presented matches the persisted token (constant
// time) and, on success, deletes the file so it cannot be used twice.
//
// Returns (false, nil) if the file is absent (no setup pending - every
// non-error "no" case maps to this) or if the token doesn't match. Returns
// (true, nil) on successful consumption.
func ConsumeSetupToken(policyRoot, presented string) (bool, error) {
	path := SetupTokenPath(policyRoot)
	stored, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	storedHex := strings.TrimSpace(string(stored))
	presentedHex := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(presented), "-", ""))
	if subtle.ConstantTimeCompare([]byte(storedHex), []byte(presentedHex)) != 1 {
		return false, nil
	}
	// One-shot: delete the file so a second submit can't re-use it.
	if err := os.Remove(path); err != nil {
		// File missing during a race? Treat as already-consumed.
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

// DeleteSetupToken removes the file if present. Used when the first admin
// user is created via a different path (CLI seed, future commercial bootstrap)
// to prevent a leftover token from being claimable.
func DeleteSetupToken(policyRoot string) error {
	err := os.Remove(SetupTokenPath(policyRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// formatHex turns a 16-char hex string into XXXX-XXXX-XXXX-XXXX uppercase.
func formatHex(hexStr string) string {
	hexStr = strings.ToUpper(hexStr)
	if len(hexStr) != 16 {
		return hexStr // shouldn't happen, but don't crash
	}
	return fmt.Sprintf("%s-%s-%s-%s", hexStr[0:4], hexStr[4:8], hexStr[8:12], hexStr[12:16])
}

// validateAndFormat accepts either the raw 16-hex form (as persisted) or the
// dashed user-facing form, and returns the user-facing form. Returns false if
// the input doesn't look like a 64-bit hex token.
func validateAndFormat(s string) (string, bool) {
	clean := strings.ToLower(strings.ReplaceAll(s, "-", ""))
	if len(clean) != 16 {
		return "", false
	}
	if _, err := hex.DecodeString(clean); err != nil {
		return "", false
	}
	return formatHex(clean), true
}
