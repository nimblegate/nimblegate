// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package render produces the markdown body of the PR sticky comment +
// embeds the hidden JSON data block. Pure function over a notification.
package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"nimblegate/internal/gateway/notification"
)

// MarkerStart is the prefix of the hidden HTML comment that wraps the
// machine-parseable JSON payload at the end of every rendered comment.
// Adapter's ScanForMarker scans for this prefix when recovering a sticky
// comment without a known ID (state lost / fresh box).
const MarkerStart = "<!-- nimblegate-data:"

// Comment returns the full markdown body (rendered for humans + hidden
// nimblegate-data block for agents) for a single notification. Status line
// at top selects the scenario:
//
//	⛔ Push rejected (default)
//	🔄 (rotation banner inline when Mention.Rotation.RotatedFrom is set)
//	⛔⛔ Loop exhausted (when Mention.FallbackActive == true)
//	⚠ OBSERVE (when Event == "push.observed")
func Comment(n notification.Notification) string {
	if n.Event == "push.resolved" {
		return resolvedComment(n)
	}
	var b strings.Builder
	writeStatusHeader(&b, n)
	writeRotationBanner(&b, n)
	writeExhaustionMessage(&b, n)
	writeMentionLine(&b, n)
	writeFindings(&b, n)
	writeHistory(&b, n)
	writeNextSteps(&b, n)
	writeFooter(&b, n)
	writeHiddenData(&b, n)
	return b.String()
}

// resolvedComment renders the sticky comment's resolved state: a clean push
// closed the fix-loop. Keeps the footer + hidden data block (so the sticky
// identity / marker is preserved) but drops the findings table and mention.
func resolvedComment(n notification.Notification) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## ✅ All findings resolved - push accepted")
	fmt.Fprintln(&b)
	branch := ""
	if len(n.Push.Refs) > 0 {
		branch = n.Push.Refs[0].Name
	}
	prField := ""
	if n.Push.PR != nil {
		prField = fmt.Sprintf(" · **PR:** #%d", n.Push.PR.Number)
	}
	fmt.Fprintf(&b, "**Repo:** `%s` · **Branch:** `%s`%s\n\n", n.Repo.Name, branch, prField)
	fmt.Fprintln(&b, "The gateway re-checked the latest push and found no blocking findings. This fix-loop is closed.")
	fmt.Fprintln(&b)
	writeFooter(&b, n)
	writeHiddenData(&b, n)
	return b.String()
}

func writeStatusHeader(b *strings.Builder, n notification.Notification) {
	statusLine := "## ⛔ Push rejected by nimblegate gateway"
	switch {
	case n.Event == "push.observed":
		statusLine = "## ⚠ OBSERVE mode · would have rejected (push accepted + relayed)"
	case n.Mention != nil && n.Mention.FallbackActive:
		statusLine = "## ⛔⛔ Loop exhausted"
	}
	fmt.Fprintln(b, statusLine)
	fmt.Fprintln(b)

	branch := ""
	if len(n.Push.Refs) > 0 {
		branch = n.Push.Refs[0].Name
	}
	prField := ""
	if n.Push.PR != nil {
		prField = fmt.Sprintf(" · **PR:** #%d", n.Push.PR.Number)
	}
	fmt.Fprintf(b, "**Repo:** `%s` · **Branch:** `%s`%s\n", n.Repo.Name, branch, prField)
	if n.Push.PusherKeyFingerprint != "" {
		fmt.Fprintf(b, "Pusher key: `%s` · Push at: %s\n", n.Push.PusherKeyFingerprint, n.Push.Timestamp.UTC().Format("2006-01-02 15:04 UTC"))
	}
	fmt.Fprintln(b)
}

func writeRotationBanner(b *strings.Builder, n notification.Notification) {
	if n.Mention == nil || n.Mention.Rotation == nil || n.Mention.Rotation.RotatedFrom == "" {
		return
	}
	r := n.Mention.Rotation
	reason := r.RotatedReason
	switch r.RotatedReason {
	case "same-finding":
		reason = "same finding as previous attempt - agent not making progress"
	case "attempt-threshold":
		reason = fmt.Sprintf("%d attempts without fix", r.AttemptIndex-1)
	case "exhaustion":
		reason = "all bots tried - human review needed"
	}
	fmt.Fprintf(b, "🔄 **Rotated from %s** → %s - %s.\n\n", r.RotatedFrom, n.Mention.CurrentBot, reason)
}

func writeExhaustionMessage(b *strings.Builder, n notification.Notification) {
	if n.Mention == nil || !n.Mention.FallbackActive {
		return
	}
	fmt.Fprintf(b, "%s - automated fix-loop exhausted on this PR. Human review required.\n", n.Mention.CurrentBot)
	if n.LoopState != nil {
		fmt.Fprintf(b, "The agents tried %d times. The same finding persists - either the fix needs context the bots don't have, or a rule needs review.\n", n.LoopState.PRAttemptCount)
	}
	fmt.Fprintln(b)
}

func writeMentionLine(b *strings.Builder, n notification.Notification) {
	if n.Mention == nil {
		return
	}
	if n.Mention.FallbackActive {
		// Already mentioned in exhaustion message; don't double-tag.
		return
	}
	parts := []string{}
	if n.Mention.CurrentBot != "" {
		parts = append(parts, n.Mention.CurrentBot)
	}
	for _, h := range n.Mention.AutoTaggedHumans {
		parts = append(parts, h+" (assigned)")
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintln(b, strings.Join(parts, " · "))
	fmt.Fprintln(b)
}

func writeFindings(b *strings.Builder, n notification.Notification) {
	if len(n.Decision.Findings) == 0 {
		return
	}
	blocks, warns, infos := groupBySeverity(n.Decision.Findings)
	headerPrefix := "Must fix"
	if n.Event == "push.observed" {
		headerPrefix = "Would have blocked"
	}
	if len(blocks) > 0 {
		fmt.Fprintf(b, "### %s (%d BLOCK):\n\n", headerPrefix, len(blocks))
		writeFindingsTable(b, blocks, "🔴 BLOCK")
		fmt.Fprintln(b)
	}
	if len(warns) > 0 {
		fmt.Fprintf(b, "### Worth fixing while you're in here (%d WARN):\n\n", len(warns))
		writeFindingsTable(b, warns, "🟡 WARN")
		fmt.Fprintln(b)
	}
	if len(infos) > 0 {
		fmt.Fprintf(b, "### Informational (%d INFO):\n\n", len(infos))
		writeFindingsTable(b, infos, "🔵 INFO")
		fmt.Fprintln(b)
	}
}

func writeFindingsTable(b *strings.Builder, findings []notification.Finding, sevLabel string) {
	fmt.Fprintln(b, "| Severity | Frame | Location | Reason | Hint |")
	fmt.Fprintln(b, "|---|---|---|---|---|")
	for _, f := range findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(b, "| %s | `%s` | `%s` | %s | %s |\n", sevLabel, f.FrameID, loc, f.Message, f.Hint)
	}
}

func writeHistory(b *strings.Builder, n notification.Notification) {
	if n.LoopState == nil || len(n.LoopState.PreviousAttempts) == 0 {
		return
	}
	fmt.Fprintf(b, "<details>\n<summary>▾ Previous attempts (%d - click to expand)</summary>\n\n", len(n.LoopState.PreviousAttempts))
	for _, a := range n.LoopState.PreviousAttempts {
		bot := a.Bot
		if bot == "" {
			bot = "-"
		}
		fmt.Fprintf(b, "- `%s` · %s · %s\n", a.SHA, a.Timestamp.UTC().Format("2006-01-02 15:04 UTC"), bot)
	}
	fmt.Fprintln(b, "\n</details>")
	fmt.Fprintln(b)
}

func writeNextSteps(b *strings.Builder, n notification.Notification) {
	if n.Event == "push.observed" || (n.Mention != nil && n.Mention.FallbackActive) {
		return
	}
	fmt.Fprintln(b, "### Next steps")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "- Fix the BLOCK findings and `git push` again - gateway re-evaluates the new tree.")
	if n.Mention != nil && n.Mention.Default != "" {
		fmt.Fprintf(b, "- Mention `%s` for explicit re-check.\n", n.Mention.Default)
	}
	fmt.Fprintln(b)
}

func writeFooter(b *strings.Builder, n notification.Notification) {
	attempt := ""
	if n.LoopState != nil && n.LoopState.PRAttemptCount > 0 {
		attempt = fmt.Sprintf(" · attempt %d/%d", n.LoopState.PRAttemptCount, n.LoopState.MaxAttempts)
	}
	fmt.Fprintln(b, "---")
	fmt.Fprintf(b, "*Posted by nimblegate %s · %s%s*\n", n.Gateway.Version, n.Gateway.InstanceID, attempt)
}

func writeHiddenData(b *strings.Builder, n notification.Notification) {
	data, _ := json.Marshal(n)
	fmt.Fprintln(b)
	fmt.Fprintln(b, MarkerStart)
	b.Write(data)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "-->")
}

func groupBySeverity(findings []notification.Finding) (blocks, warns, infos []notification.Finding) {
	for _, f := range findings {
		switch f.Severity {
		case "BLOCK", "ERROR":
			blocks = append(blocks, f)
		case "WARN":
			warns = append(warns, f)
		case "INFO":
			infos = append(infos, f)
		}
	}
	return
}
