// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"path/filepath"
	"strings"
	"time"
)

// Suppressor filters whitelisted hits from raw CheckResults BEFORE the
// dedup pass. The audit log writes raw (pre-suppression) results, so a
// whitelisted hit is still recorded - every bypass is auditable.
//
// Implementations live in package whitelist; this interface keeps the
// engine package free of a dependency on whitelist (callers wire the
// concrete type in at the trigger layer).
type Suppressor interface {
	// Match returns true if (frameID, file, label) is covered by an
	// active whitelist entry. File is the project-relative path; the
	// caller converts absolute paths via filepath.Rel before calling.
	Match(frameID, file, label string) bool
}

// SuppressionLog is appended to by ApplyWhitelist for every Hit
// suppressed. Callers persist it to the audit trail so a future
// "why didn't this fire?" question has an answer.
type SuppressionLog struct {
	Timestamp time.Time
	FrameID   string
	File      string
	Label     string
}

// ApplyWhitelist filters Hits from raw results that are covered by a
// whitelist entry. Returns a new slice of CheckResult (input is not
// mutated) plus the list of suppressions for audit. ProjectRoot is used
// to convert absolute Hit.File paths to project-relative for matching.
//
// Behavior per result:
//
//   - len(Hits) == 0  → passed through unchanged (no structured location
//     to match against; frame-level results aren't suppressible via the
//     whitelist mechanism)
//   - all Hits suppressed → Outcome demoted to OutcomePass, Reason set
//     to a one-line note, Hits cleared
//   - some Hits suppressed → Hits filtered, Reason rebuilt from the
//     remaining Hits using the original "<header>: <hits...>" shape
//   - no Hits suppressed → unchanged
//
// When w is nil (no whitelist loaded), returns (results, nil) unchanged.
func ApplyWhitelist(results []CheckResult, w Suppressor, projectRoot string) ([]CheckResult, []SuppressionLog) {
	if w == nil {
		return results, nil
	}
	out := make([]CheckResult, len(results))
	var log []SuppressionLog
	now := time.Now().UTC()

	for i, r := range results {
		out[i] = r
		if len(r.Hits) == 0 {
			continue
		}
		kept := make([]Hit, 0, len(r.Hits))
		dropped := 0
		for _, h := range r.Hits {
			rel := relPath(h.File, projectRoot)
			if w.Match(r.FrameID, rel, h.Label) {
				dropped++
				log = append(log, SuppressionLog{
					Timestamp: now,
					FrameID:   r.FrameID,
					File:      rel,
					Label:     h.Label,
				})
				continue
			}
			kept = append(kept, h)
		}
		if dropped == 0 {
			continue
		}
		out[i].Hits = kept
		switch {
		case len(kept) == 0:
			// Every hit was whitelisted. Demote to PASS so the gate doesn't
			// fail on a fully-exempted finding. Reason is a transparency
			// note - the user should still understand WHY this frame is
			// quiet despite there being matchable content.
			out[i].Outcome = OutcomePass
			out[i].Reason = "all findings suppressed by whitelist (see audit log)"
			out[i].Fix = ""
		default:
			// Partial - rebuild Reason from the surviving Hits using the
			// existing format: "<header>: hit; hit; hit".
			out[i].Reason = rebuildReason(r.Reason, kept)
		}
	}
	return out, log
}

// rebuildReason replaces the hit list in the original Reason text with
// the formatted versions of the surviving Hits. The split point is the
// first ": " (colon-space) - the convention every Hit-producing frame
// follows ("<header>: hit; hit"). If the format is unrecognized, the
// original Reason is returned unchanged (safer than corrupting it).
func rebuildReason(original string, kept []Hit) string {
	idx := strings.Index(original, ": ")
	if idx < 0 {
		return original
	}
	header := original[:idx]
	parts := make([]string, len(kept))
	for i, h := range kept {
		parts[i] = h.Format()
	}
	return header + ": " + strings.Join(parts, "; ")
}

// relPath converts an absolute Hit.File to project-relative for glob
// matching. Falls back to the original string on error (e.g., if the
// path is not under projectRoot) so the matcher can still try a literal
// match.
func relPath(file, projectRoot string) string {
	if projectRoot == "" || !filepath.IsAbs(file) {
		return file
	}
	rel, err := filepath.Rel(projectRoot, file)
	if err != nil {
		return file
	}
	return rel
}
