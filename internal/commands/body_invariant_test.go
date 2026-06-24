// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInvariant_FrameBodyNeverReachesStdout - Frame.Body is set by the
// parser but should be entirely unused at runtime. A frame body could
// contain malicious URLs, ANSI escapes, HTML, javascript: links etc.;
// this test embeds a unique sentinel string in a project frame's body
// and runs every read-only CLI command, asserting the sentinel never
// appears in stdout or stderr.
//
// If this test starts failing, a NEW command has started printing
// frame bodies. Make sure that command sanitizes the body and renders
// links safely (no auto-fetch, no HTML injection, no clickable
// terminal hyperlinks pointing at user-supplied URLs).
func TestInvariant_FrameBodyNeverReachesStdout(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}

	// A frame body packed with everything a malicious author might try:
	// scheme-y URLs, ANSI clear-screen, fake clickable hyperlinks,
	// javascript: schemes, data: URIs, and a unique sentinel that should
	// never appear in CLI output.
	sentinel := "BODY-LEAK-DETECTOR-9c1f8e7d-canary"
	frame := `---
name: with-body
category: security
severity: INFO
triggers: [cli]
---

# Heading

` + sentinel + `

Hostile content for the test:
- https://evil.example.com/track?u=` + sentinel + `
- [click](javascript:alert(1))
- [data](data:text/html,<script>` + sentinel + `</script>)
- ` + "\x1b[2J\x1b[H" + ` : bare ANSI clear screen
- <a href="https://attacker">link</a>
`
	dir := filepath.Join(tmp, ".appframes", "security")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "with-body.md"), []byte(frame), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds := [][]string{
		{"check"},
		{"check", "--trigger=cli"},
		{"check", "--trigger=pre-commit"},
		{"lint"},
		{"status"},
		{"shell", "print", "--shell=bash"},
	}
	for _, args := range cmds {
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		out, _ := cmd.CombinedOutput()
		if strings.Contains(string(out), sentinel) {
			t.Errorf("command %v leaked frame body to stdout/stderr (sentinel found):\n%s", args, out)
		}
	}
}
