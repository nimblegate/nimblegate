// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package auth

import "testing"

// ReadSetupToken is the read-only retrieval the `nimblegate gateway setup-token`
// command uses: show the pending token if one exists, never generate or
// consume. It must track the token lifecycle - absent before first start,
// present (and matching) after EnsureSetupToken, absent again after the admin
// claims it (the token is one-shot, deleted on consume).
func TestReadSetupToken_lifecycle(t *testing.T) {
	root := t.TempDir()

	// Before the dashboard ever ran: no token file → not present, no error.
	if tok, present, err := ReadSetupToken(root); err != nil || present || tok != "" {
		t.Fatalf("absent: tok=%q present=%v err=%v; want \"\", false, nil", tok, present, err)
	}

	// After first start generates it: present and identical to what the
	// dashboard would display/log.
	want, _, err := EnsureSetupToken(root)
	if err != nil {
		t.Fatal(err)
	}
	got, present, err := ReadSetupToken(root)
	if err != nil || !present {
		t.Fatalf("present: present=%v err=%v; want true, nil", present, err)
	}
	if got != want {
		t.Errorf("ReadSetupToken=%q, EnsureSetupToken=%q - must match", got, want)
	}

	// After the admin claims it: one-shot delete → not present again.
	ok, err := ConsumeSetupToken(root, want)
	if err != nil || !ok {
		t.Fatalf("consume: ok=%v err=%v", ok, err)
	}
	if _, present, _ := ReadSetupToken(root); present {
		t.Error("after claim, ReadSetupToken must report no pending token")
	}
}

func TestReadSetupToken_emptyRoot(t *testing.T) {
	if _, _, err := ReadSetupToken(""); err == nil {
		t.Error("ReadSetupToken(\"\") must error")
	}
}
