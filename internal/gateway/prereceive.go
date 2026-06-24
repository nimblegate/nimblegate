// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway/notification"
)

// PreReceiveDeps are the injected dependencies for one repo's pre-receive run.
type PreReceiveDeps struct {
	Policy    Policy
	GitDir    string // the gateway bare repo (GIT_DIR)
	Checker   Checker
	AuditPath string
	// Notification rail (all optional - nil/empty disables the rail for this push).
	NotificationConfig *NotificationConfig
	Orchestrator       *notification.Orchestrator // for inline attempt; nil = skip inline, daemon drains
	GatewayVersion     string                     // e.g. "v0.1.0"
	InstanceID         string                     // hostname / instance label
	PolicyRoot         string                     // for queue file location + PRState
}

// NotificationConfig is the per-repo notification rail config. Pre-receive
// only reads the rail-decision fields (Enabled, UpstreamKind, WebhookURL,
// WebhookAuth); the daemon + orchestrator + dashboard read the rest
// (Mention, Delivery, Cooldown, LoopCfg). One type, multiple consumers -
// each grabs the fields it needs.
type NotificationConfig struct {
	Enabled           bool
	ObservePRComments bool   // mirrors observe-mode for the PR-comment side of the rail
	UpstreamKind      string // "gitea" | "github" - derived from upstream URL by caller, not parsed
	WebhookURL        string // empty = webhook disabled, comment-only
	WebhookAuth       notification.WebhookAuth
	LoopCfg           notification.LoopConfig     // [notification.loop] + [notification.mention.rotation]
	Cooldown          notification.CooldownConfig // [notification.loop] cooldown subset
	Mention           MentionConfig               // [notification.mention]
	Delivery          DeliveryConfig              // [notification.delivery]
}

// MentionConfig is the per-repo PR-comment mention config (spec §7.1
// [notification.mention] + [notification.mention.rotation]).
type MentionConfig struct {
	Default               string   // fallback bot handle when rotation is disabled
	IncludePRAssignees    bool     // append PR assignees to the mention line
	RotationBots          []string // empty = rotation disabled
	AttemptsPerBot        int
	RotateOnRepeatFinding bool
	FallbackHuman         string // empty = no human fallback
}

// DeliveryConfig is the daemon's per-repo retry/backoff config
// (spec §7.1 [notification.delivery]).
type DeliveryConfig struct {
	MaxAttempts     int
	BackoffSchedule []time.Duration
}

// inlineDeliveryTimeout caps the opportunistic inline DeliverOne in pre-receive.
// Failure / timeout is silent - the daemon will drain the queue record.
const inlineDeliveryTimeout = 3 * time.Second

// parseRefLines reads "<old> <new> <ref>" lines from a hook's stdin.
func parseRefLines(r io.Reader) ([]RefUpdate, error) {
	var out []RefUpdate
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 3 {
			continue
		}
		out = append(out, RefUpdate{OldRev: f[0], NewRev: f[1], Name: f[2]})
	}
	return out, sc.Err()
}

// RunPreReceive gates a push. Returns a process exit code (0 = accept).
func RunPreReceive(d PreReceiveDeps, stdin io.Reader, stdout io.Writer) int {
	refs, err := parseRefLines(stdin)
	if err != nil {
		fmt.Fprintf(stdout, "error: cannot read refs: %v\n", err)
		return 1
	}
	resultsByRef := map[string][]engine.CheckResult{}
	suppByRef := map[string][]Suppression{}
	if d.Policy.Enabled {
		for _, r := range refs {
			if !isGatedRef(d.Policy, r.Name) || r.IsDelete() {
				continue // non-gated: no check; delete handled by Decide
			}
			tmp, err := os.MkdirTemp("", "afgw-")
			if err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: "gateway", Outcome: engine.OutcomeError, Reason: err.Error()}}
				continue
			}
			defer os.RemoveAll(tmp)
			if err := materializeTree(d.GitDir, r.NewRev, tmp); err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: "gateway", Outcome: engine.OutcomeError, Reason: "materialize: " + err.Error()}}
				continue
			}
			if err := overlayPolicy(d.Policy.PolicyDir, tmp); err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: "gateway", Outcome: engine.OutcomeError, Reason: "overlay: " + err.Error()}}
				continue
			}
			res, supp, err := d.Checker.Check(tmp)
			if err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: "gateway", Outcome: engine.OutcomeError, Reason: "check: " + err.Error()}}
				continue
			}
			resultsByRef[r.Name] = relativizeResults(res, tmp)
			suppByRef[r.Name] = relativizeSuppressions(supp, tmp)
		}
	}

	dec := Decide(d.Policy, refs, resultsByRef)

	refNames := make([]string, 0, len(refs))
	for _, r := range refs {
		refNames = append(refNames, r.Name)
	}
	// Observe mode: a would-block is recorded but relayed anyway (advisory). The
	// record's Accept reflects the real outcome (relayed = true); Observed marks
	// that it would have been rejected under enforcement.
	observed := !dec.Accept && d.Policy.Observe
	accept := dec.Accept || observed
	var suppressed []Suppression
	for _, r := range refs {
		suppressed = append(suppressed, suppByRef[r.Name]...)
	}
	// Notification rail (additive - gated on per-repo config). Pre-receive only
	// fires for true rejects under enforcement; observe-mode would-blocks are
	// relayed and tracked via the audit-log path, not the notification rail.
	// Build the notification BEFORE writing the audit record so the record can
	// carry the notification's EventID. The audit log is append-only and the
	// daemon delivers asynchronously, so the record stores only "a notification
	// fired (this EventID)"; the dashboard recovers the live outcome
	// (delivered / queued / deadlettered) at read time by cross-referencing the
	// EventID against the queue + deadletter files (ReadDecisions correlation).
	notifEnabled := d.NotificationConfig != nil && d.NotificationConfig.Enabled
	willNotify := !dec.Accept && !d.Policy.Observe && notifEnabled

	var notif notification.Notification
	var notifStatus *NotificationStatus
	var resRecs []notification.QueueRecord // resolution records (clean push closing loops)
	var resPRs []int
	switch {
	case willNotify:
		notif = buildNotification(d, refs, dec, suppressed)
		notifStatus = &NotificationStatus{EventID: notif.EventID, QueuedAt: notif.Push.Timestamp}
	case dec.Accept && notifEnabled:
		// Clean push to a gated ref = the agent's fix converged. Build a
		// resolution per active loop on the accepted refs and stamp the first
		// onto the audit record, so the resolution also shows in the feed +
		// Activity tab (an accepted row carrying a notification is, uniquely, a
		// resolution - observe-mode and plain clean pushes don't fire the rail).
		resRecs, resPRs = buildResolutions(d, refs)
		if len(resRecs) > 0 {
			notifStatus = &NotificationStatus{EventID: resRecs[0].ID, QueuedAt: resRecs[0].Notification.Push.Timestamp}
		}
	}

	_ = AppendAudit(d.AuditPath, AuditRecord{Repo: d.Policy.Repo, Refs: refNames, RefUpdates: refs, Accept: accept, Observed: observed, Messages: dec.Messages, Findings: dec.Findings, Suppressed: suppressed, Notification: notifStatus})

	switch {
	case willNotify:
		// A queue-write failure is an operator/notification-rail concern, not
		// the pusher's - and naming the rail would reveal the gateway. Record
		// it operator-side; the audit row already captured the decision.
		if err := enqueueNotification(d, notif); err != nil {
			_ = AppendEvent(d.PolicyRoot, Event{Event: "notification-enqueue-failed", Repo: d.Policy.Repo, OK: false, Payload: map[string]any{"error": err.Error()}})
		}
	case len(resRecs) > 0:
		enqueueResolutions(d, resRecs, resPRs)
	}

	// Every path that relays - clean accept, accept-with-suppressions, and
	// observe-mode would-block - is fully silent to the pushing client. An
	// agent under observation must see a push indistinguishable from one
	// with no gateway in front; any output here contaminates the
	// observation (the agent adapts to the observer). The audit record
	// above carries Observed + Messages + Suppressed for the operator.
	if !dec.Accept {
		if d.Policy.Observe {
			return 0
		}
		// Pusher-facing output mimics an ordinary git host's pre-receive policy
		// reject - NO branding, NO gateway/relay language. git appends its own
		// "! [remote rejected] … (pre-receive hook declined)". The whitelist
		// suppressions, repo name, and full findings are in the operator audit
		// record above; revealing them here would mark the gateway.
		fmt.Fprintln(stdout, "error: push rejected by repository policy:")
		for _, m := range dec.Messages {
			fmt.Fprintf(stdout, "  %s\n", m)
		}
		return 1
	}
	return 0
}

// relativizeResults rewrites scan-worktree-absolute paths (under root) to
// repo-relative ones in the check results, so findings reference the real repo
// file ("work.txt:1") instead of the gateway's ephemeral materialize dir
// ("/tmp/afgw-XXXX/work.txt:1", deleted right after the push). Strips the
// prefix from each Hit.File and from the pre-joined Reason string.
func relativizeResults(results []engine.CheckResult, root string) []engine.CheckResult {
	prefix := strings.TrimRight(root, string(os.PathSeparator)) + string(os.PathSeparator)
	for i := range results {
		results[i].Reason = strings.ReplaceAll(results[i].Reason, prefix, "")
		for j := range results[i].Hits {
			results[i].Hits[j].File = strings.TrimPrefix(results[i].Hits[j].File, prefix)
		}
	}
	return results
}

// buildNotification constructs the rejection notification payload. Split out
// from enqueueNotification so the caller can read the EventID and stamp it onto
// the audit record before the queue write.
func buildNotification(d PreReceiveDeps, refs []RefUpdate, dec Decision, suppressed []Suppression) notification.Notification {
	in := notification.BuildInput{
		Repo:        d.Policy.Repo,
		UpstreamURL: d.Policy.UpstreamURL,
		Observed:    false, // gated to non-observe rejects upstream of this call
		Refs:        toBuildRefs(refs),
		Findings:    toBuildFindings(dec.Findings),
		Suppressed:  toBuildSuppressions(suppressed),
	}
	return notification.Build(in, d.GatewayVersion, d.InstanceID)
}

// enqueueNotification writes the notification to the queue (durability anchor),
// then tries an opportunistic inline DeliverOne with a short timeout. Inline
// failure is silent - the daemon will drain the queue record on its next poll.
// Queue write failure is returned to the caller so the user-visible reject can
// carry a "logged for retry" line without aborting the reject itself.
func enqueueNotification(d PreReceiveDeps, notif notification.Notification) error {
	queuePath := filepath.Join(d.PolicyRoot, d.Policy.Repo, "pr-comment-queue.jsonl")
	qrec := notification.QueueRecord{
		ID:           notif.EventID,
		Notification: notif,
		UpstreamKind: d.NotificationConfig.UpstreamKind,
		WebhookURL:   d.NotificationConfig.WebhookURL,
		WebhookAuth:  d.NotificationConfig.WebhookAuth,
		// Carry the loop config so the daemon can advance the per-PR attempt
		// counter / bot rotation when it resolves the PR at delivery time.
		LoopConfig: d.NotificationConfig.LoopCfg,
	}
	if err := notification.AppendQueueRecord(queuePath, qrec); err != nil {
		return err
	}

	// Opportunistic inline attempt - only if Orchestrator was injected.
	if d.Orchestrator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), inlineDeliveryTimeout)
		defer cancel()
		if err := d.Orchestrator.DeliverOne(ctx, qrec); err == nil {
			_ = notification.RemoveQueueRecord(queuePath, qrec.ID)
		}
		// Failure path: queue record stays for the daemon to drain.
	}
	return nil
}

// buildResolutions returns a "push.resolved" queue record for each active loop
// on the accepted gated refs (a clean push just landed there), plus the PR
// numbers whose loop state should be cleared. Each record carries the sticky
// comment id so the daemon can update the existing PR comment to ✅. Built
// before the audit write so the caller can stamp a resolution's EventID onto
// the audit record.
func buildResolutions(d PreReceiveDeps, refs []RefUpdate) ([]notification.QueueRecord, []int) {
	var recs []notification.QueueRecord
	var prNums []int
	for _, r := range refs {
		if r.IsDelete() || !isGatedRef(d.Policy, r.Name) {
			continue
		}
		states, _ := notification.ListLoopsForRef(d.PolicyRoot, d.Policy.Repo, r.Name)
		for _, st := range states {
			notif := notification.Build(notification.BuildInput{
				Repo:        d.Policy.Repo,
				UpstreamURL: d.Policy.UpstreamURL,
				Resolved:    true,
				Refs:        []notification.BuildRef{{Name: r.Name}},
			}, d.GatewayVersion, d.InstanceID)
			recs = append(recs, notification.QueueRecord{
				ID:           notif.EventID,
				Notification: notif,
				UpstreamKind: d.NotificationConfig.UpstreamKind,
				WebhookURL:   d.NotificationConfig.WebhookURL,
				WebhookAuth:  d.NotificationConfig.WebhookAuth,
				State: notification.QueueRecordState{
					PRNumber:        st.PRNumber,
					StickyCommentID: st.StickyComment.ID,
				},
			})
			prNums = append(prNums, st.PRNumber)
		}
	}
	return recs, prNums
}

// enqueueResolutions writes the resolution records to the queue and clears each
// PR's loop state so the dashboard drops it immediately. Best-effort: if an
// enqueue fails, that PR's loop state is left in place so a later push retries.
func enqueueResolutions(d PreReceiveDeps, recs []notification.QueueRecord, prNums []int) {
	queuePath := filepath.Join(d.PolicyRoot, d.Policy.Repo, "pr-comment-queue.jsonl")
	for i, qrec := range recs {
		if err := notification.AppendQueueRecord(queuePath, qrec); err != nil {
			continue
		}
		_ = notification.DeletePRState(d.PolicyRoot, d.Policy.Repo, prNums[i])
		// Fix B: once this ref's loop is resolved, drop any reject records still
		// pending for it. Delivering a stale reject after the resolution would
		// re-open the loop (resolution cleared the PR state → fresh attempt 1) and
		// flip the ✅ comment back to ⛔. The resolution record just appended is
		// preserved (it's the only push.resolved record for the ref).
		if refs := qrec.Notification.Push.Refs; len(refs) > 0 {
			_, _ = notification.RemovePendingRejectsForRef(queuePath, refs[0].Name)
		}
	}
}

func toBuildRefs(refs []RefUpdate) []notification.BuildRef {
	out := make([]notification.BuildRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, notification.BuildRef{Name: r.Name, OldSHA: r.OldRev, NewSHA: r.NewRev})
	}
	return out
}

func toBuildFindings(findings []Finding) []notification.BuildFinding {
	out := make([]notification.BuildFinding, 0, len(findings))
	for _, f := range findings {
		out = append(out, notification.BuildFinding{FrameID: f.ID, Severity: f.Severity, Message: f.Message})
	}
	return out
}

func toBuildSuppressions(supp []Suppression) []notification.BuildSuppression {
	out := make([]notification.BuildSuppression, 0, len(supp))
	for _, s := range supp {
		out = append(out, notification.BuildSuppression{FrameID: s.Frame, File: s.File, Label: s.Label})
	}
	return out
}

// relativizeSuppressions strips the materialize-dir prefix from each
// suppression's File, mirroring relativizeResults, and maps the engine log to
// the audit-facing Suppression shape.
func relativizeSuppressions(logs []engine.SuppressionLog, root string) []Suppression {
	prefix := strings.TrimRight(root, string(os.PathSeparator)) + string(os.PathSeparator)
	out := make([]Suppression, 0, len(logs))
	for _, l := range logs {
		out = append(out, Suppression{Frame: l.FrameID, File: strings.TrimPrefix(l.File, prefix), Label: l.Label})
	}
	return out
}
