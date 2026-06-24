// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runLineEndingsCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return ConsistentLineEndings(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestConsistentLineEndings_MixedBlocks(t *testing.T) {
	body := "line1\r\nline2\nline3\r\nline4\n"
	got := runLineEndingsCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "mixed") {
		t.Errorf("reason should mention mixed; got: %s", got.Reason)
	}
}

func TestConsistentLineEndings_ShebangCRLFBlocks(t *testing.T) {
	body := "#!/bin/bash\r\necho hi\r\n"
	got := runLineEndingsCheck(t, "run.sh", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "shebang") {
		t.Errorf("reason should mention shebang; got: %s", got.Reason)
	}
}

func TestConsistentLineEndings_AllLFPasses(t *testing.T) {
	body := "line1\nline2\nline3\n"
	got := runLineEndingsCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestConsistentLineEndings_AllCRLFPasses(t *testing.T) {
	body := "line1\r\nline2\r\nline3\r\n"
	got := runLineEndingsCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("pure CRLF (no shebang) should pass; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestConsistentLineEndings_BatExempt(t *testing.T) {
	body := "echo hi\r\necho bye\n"
	got := runLineEndingsCheck(t, "run.bat", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".bat is exempt; got %s reason=%s", got.Outcome, got.Reason)
	}
}

// TestConsistentLineEndings_BinaryExtensionsSkipped is the load-bearing
// regression for the Phase G Task G2 fix. Files with binary extensions
// (PNG, JPG, fonts, etc.) must not trigger the line-ending check even
// when their byte content happens to contain CRLF + LF mixtures -
// that's normal for binary content, not a real line-ending issue.
//
// Per the multi-kit comparison validation file: pre-fix, the encoding-
// strict kit produced 59 false-positive BLOCK events on og-image.png
// across myapp's 197-commit history.
func TestConsistentLineEndings_BinaryExtensionsSkipped(t *testing.T) {
	// Construct fake binary-ish content: a mix of CR and LF bytes (which
	// would trip the check on a text file).
	bin := "\x89PNG\r\n\x1a\nfake-png-content\r\nmore-bytes\n"
	for _, ext := range []string{".png", ".jpg", ".woff2", ".pdf", ".zip", ".mp4"} {
		got := runLineEndingsCheck(t, "asset"+ext, bin)
		if got.Outcome != engine.OutcomePass {
			t.Errorf("%s should be skipped (binary extension); got %s reason=%s", ext, got.Outcome, got.Reason)
		}
	}
}

// TestConsistentLineEndings_MinifiedJsSkipped covers vendor minified
// JavaScript like jspdf.umd.min.js - operator-uncontrolled bundled code
// where line-ending consistency isn't enforceable.
func TestConsistentLineEndings_MinifiedJsSkipped(t *testing.T) {
	body := "var a=1;\r\nvar b=2;\nvar c=3;\r\n"
	got := runLineEndingsCheck(t, "vendor/lib.min.js", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".min.js should be skipped (minified vendor code); got %s reason=%s", got.Outcome, got.Reason)
	}
}

// TestConsistentLineEndings_BinaryContentSkipped covers files with text-
// like extensions but binary content (e.g., a generic file with NULL
// bytes or high non-printable ratio).
func TestConsistentLineEndings_BinaryContentSkipped(t *testing.T) {
	// 100 bytes with a NULL byte mixed in - should be detected as binary
	// regardless of extension.
	bin := strings.Repeat("\x00\x01\x02\x03\x04", 20) + "\r\nfake\n"
	got := runLineEndingsCheck(t, "data.dat", bin) // .dat isn't in skip list
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file with NULL bytes should be skipped (content-detect); got %s reason=%s", got.Outcome, got.Reason)
	}
}

// TestIsLikelyBinaryContent covers the content-based heuristic directly.
func TestIsLikelyBinaryContent(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", []byte{}, false},
		{"plain text", []byte("hello world\n"), false},
		{"text with high-bit UTF-8", []byte("héllo wörld\n"), false},
		{"NULL byte means binary", []byte("text\x00more"), true},
		{"high non-printable ratio", []byte("\x01\x02\x03\x04\x05\x01\x02\x03\x04\x05abcd"), true},
	}
	for _, c := range cases {
		if got := isLikelyBinaryContent(c.data); got != c.want {
			t.Errorf("%s: isLikelyBinaryContent = %v, want %v", c.name, got, c.want)
		}
	}
}
