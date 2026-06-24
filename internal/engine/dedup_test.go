// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"bytes"
	"strings"
	"testing"

	"nimblegate/internal/frames"
)

func TestPresentResults_PassthroughWhenDedupKeyEmpty(t *testing.T) {
	results := []CheckResult{
		{
			FrameID:  "git-safety/no-force-push-main",
			Category: frames.CategoryGitSafety,
			Outcome:  OutcomeBlock,
			Reason:   "force-push to main detected",
			DedupKey: "", // no opt-in
			Hits: []Hit{
				{File: "/repo/main", Line: 1, Label: "force-push"},
			},
		},
	}
	rows := PresentResults(results)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Reason != "force-push to main detected" {
		t.Errorf("passthrough should preserve Reason; got %q", rows[0].Reason)
	}
	if len(rows[0].FrameIDs) != 1 || rows[0].FrameIDs[0] != "git-safety/no-force-push-main" {
		t.Errorf("passthrough FrameIDs = %v, want [git-safety/no-force-push-main]", rows[0].FrameIDs)
	}
}

func TestPresentResults_PassthroughWhenHitsEmpty(t *testing.T) {
	// dedup-key set but no Hits → still passthrough (can't dedup what we
	// don't know location of).
	results := []CheckResult{
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "credential leak (legacy reason-only)",
			DedupKey: "file:line",
			Hits:     nil,
		},
	}
	rows := PresentResults(results)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Reason != "credential leak (legacy reason-only)" {
		t.Error("hits-empty should fall through to passthrough rendering")
	}
}

func TestPresentResults_DedupCollapsesAtSameScope(t *testing.T) {
	// Two frames both fire on /repo/keys/server.pem:1 with dedup-key file:line.
	// Expected: ONE row listing both frame IDs.
	results := []CheckResult{
		{
			FrameID:  "security/no-private-keys-in-repo",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "private keys detected: /repo/keys/server.pem:1 - PEM RSA private key",
			Fix:      "rotate immediately",
			DedupKey: "file:line",
			Hits: []Hit{
				{File: "/repo/keys/server.pem", Line: 1, Label: "PEM RSA private key"},
			},
		},
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "credentials detected: /repo/keys/server.pem:1 - pattern X",
			Fix:      "rotate the credential",
			DedupKey: "file:line",
			Hits: []Hit{
				{File: "/repo/keys/server.pem", Line: 1, Label: "PEM-armored key body"},
			},
		},
	}
	rows := PresentResults(results)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (collapsed)", len(rows))
	}
	row := rows[0]
	if row.Outcome != OutcomeBlock {
		t.Errorf("collapsed Outcome = %s, want BLOCK", row.Outcome)
	}
	if len(row.FrameIDs) != 2 {
		t.Errorf("FrameIDs = %v, want both frames", row.FrameIDs)
	}
	// Sorted: no-hardcoded-credentials < no-private-keys
	if row.FrameIDs[0] != "security/no-hardcoded-credentials" || row.FrameIDs[1] != "security/no-private-keys-in-repo" {
		t.Errorf("FrameIDs not sorted; got %v", row.FrameIDs)
	}
	if !strings.Contains(row.Reason, "/repo/keys/server.pem:1") {
		t.Errorf("Reason missing file:line; got %q", row.Reason)
	}
	if !strings.Contains(row.Reason, "shared by:") {
		t.Errorf("Reason should signal multi-frame dedup; got %q", row.Reason)
	}
	if row.Fix == "" {
		t.Error("Fix should be the first non-empty contributor's fix")
	}
}

func TestPresentResults_DedupKeepsDistinctLines(t *testing.T) {
	// Same file, different line numbers → two separate rows, not collapsed.
	results := []CheckResult{
		{
			FrameID:  "security/no-private-keys-in-repo",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file:line",
			Hits: []Hit{
				{File: "/repo/secrets.env", Line: 1, Label: "PEM RSA private key"},
			},
		},
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file:line",
			Hits: []Hit{
				{File: "/repo/secrets.env", Line: 42, Label: "AWS access key"},
			},
		},
	}
	rows := PresentResults(results)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (different lines = different scopes)", len(rows))
	}
}

func TestPresentResults_DedupFileScopeCollapsesAllLines(t *testing.T) {
	// dedup-key = "file" → ALL hits in same file collapse, regardless of line.
	results := []CheckResult{
		{
			FrameID:  "a/x",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file",
			Hits: []Hit{
				{File: "/repo/big.txt", Line: 5, Label: "hit a"},
			},
		},
		{
			FrameID:  "b/y",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file",
			Hits: []Hit{
				{File: "/repo/big.txt", Line: 99, Label: "hit b"},
			},
		},
	}
	rows := PresentResults(results)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (file-scope merges all lines)", len(rows))
	}
}

func TestPresentResults_DedupWorstOfOutcome(t *testing.T) {
	// One frame INFO, one frame BLOCK at the same scope → row reports BLOCK.
	results := []CheckResult{
		{
			FrameID:  "a/info",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeInfo,
			DedupKey: "file:line",
			Hits:     []Hit{{File: "/r/f", Line: 1, Label: "info"}},
		},
		{
			FrameID:  "b/block",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file:line",
			Hits:     []Hit{{File: "/r/f", Line: 1, Label: "block"}},
		},
	}
	rows := PresentResults(results)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Outcome != OutcomeBlock {
		t.Errorf("worst-of Outcome = %s, want BLOCK", rows[0].Outcome)
	}
}

func TestPresentResults_AuditUnaffectedByDedup(t *testing.T) {
	// PresentResults takes []CheckResult and returns []RenderRow. The input
	// slice must not be mutated - the caller writes raw results to the
	// audit log AFTER calling, and any mutation would corrupt the trail.
	original := []CheckResult{
		{
			FrameID:  "a/x",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file:line",
			Hits:     []Hit{{File: "/r/f", Line: 1, Label: "x"}},
		},
		{
			FrameID:  "b/y",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			DedupKey: "file:line",
			Hits:     []Hit{{File: "/r/f", Line: 1, Label: "y"}},
		},
	}
	snapshot := make([]CheckResult, len(original))
	copy(snapshot, original)

	_ = PresentResults(original)

	for i, s := range snapshot {
		if original[i].FrameID != s.FrameID || original[i].Outcome != s.Outcome {
			t.Errorf("PresentResults mutated input slice at i=%d", i)
		}
		if len(original[i].Hits) != len(s.Hits) {
			t.Errorf("PresentResults mutated Hits at i=%d", i)
		}
	}
}

// Verifies the end-to-end behavior through FormatResults: deduped output
// shows ONE BLOCK row when two frames collide on the same file:line.
func TestFormatResults_DedupEndToEnd(t *testing.T) {
	results := []CheckResult{
		{
			FrameID:  "security/no-private-keys-in-repo",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "private keys detected: keys/server.pem:1 - PEM RSA private key",
			Fix:      "rotate immediately",
			DedupKey: "file:line",
			Hits:     []Hit{{File: "keys/server.pem", Line: 1, Label: "PEM RSA private key"}},
		},
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "credentials detected: keys/server.pem:1 - PEM-armored key",
			Fix:      "rotate the credential",
			DedupKey: "file:line",
			Hits:     []Hit{{File: "keys/server.pem", Line: 1, Label: "PEM-armored key body"}},
		},
	}
	var buf bytes.Buffer
	exit := FormatResults(&buf, results)
	if exit != 1 {
		t.Errorf("exit = %d, want 1 (BLOCK present)", exit)
	}
	out := buf.String()
	// Should contain ONE ❌ line, not two.
	if blocks := strings.Count(out, "❌"); blocks != 1 {
		t.Errorf("block-row count = %d, want 1 (deduped); output:\n%s", blocks, out)
	}
	if !strings.Contains(out, "keys/server.pem:1") {
		t.Errorf("missing file:line in output; got:\n%s", out)
	}
	if !strings.Contains(out, "shared by:") {
		t.Errorf("missing dedup signal in output; got:\n%s", out)
	}
}
