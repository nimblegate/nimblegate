// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopedAccessGuard(t *testing.T) {
	dir := t.TempDir()
	keysPath := filepath.Join(dir, "authorized_keys")

	// not scoped → never blocks
	if err := scopedAccessGuard(false, keysPath); err != nil {
		t.Errorf("unscoped mode must not error: %v", err)
	}

	// scoped + a plain key present → REFUSE (fail-safe: it would bypass scoping)
	if err := os.WriteFile(keysPath, []byte(makePubkey(t, "alice")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scopedAccessGuard(true, keysPath); err == nil {
		t.Error("scoped mode with a plain (bypass-able) key must refuse to start")
	}

	// scoped + only forced-command keys → OK
	fc := `command="nimblegate gateway shell --key SHA256:x --policy-root /p --repos-root /r",restrict ` + makePubkey(t, "bob")
	if err := os.WriteFile(keysPath, []byte(fc+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scopedAccessGuard(true, keysPath); err != nil {
		t.Errorf("scoped mode with only forced-command keys should pass: %v", err)
	}

	// scoped + mixed (one plain among forced) → REFUSE
	if err := os.WriteFile(keysPath, []byte(fc+"\n"+makePubkey(t, "carol")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scopedAccessGuard(true, keysPath); err == nil {
		t.Error("a single plain key among scoped keys must still refuse")
	}

	// scoped + missing file → OK (no keys to bypass)
	if err := scopedAccessGuard(true, filepath.Join(dir, "nope")); err != nil {
		t.Errorf("missing keys file must not error: %v", err)
	}
}
