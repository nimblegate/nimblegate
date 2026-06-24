// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import "testing"

func TestFrames_DispatchesToList(t *testing.T) {
	rc := Frames([]string{"list", "--json"})
	if rc != 0 {
		t.Errorf("frames list returned %d, want 0", rc)
	}
}

func TestFrames_UnknownSubcommand(t *testing.T) {
	rc := Frames([]string{"nope"})
	if rc == 0 {
		t.Errorf("unknown subcommand should return non-zero, got %d", rc)
	}
}

func TestFrames_NoArgs(t *testing.T) {
	rc := Frames(nil)
	if rc == 0 {
		t.Errorf("no args should return non-zero usage")
	}
}
