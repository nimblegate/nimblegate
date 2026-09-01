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
	// ScanTmpDir is where the per-push materialize worktree is created. Empty
	// falls back to $TMPDIR, which is tmpfs on most distros - one full copy of
	// the pushed tree per in-flight push then lands in RAM.
	ScanTmpDir string
	// MaxTreeBytes caps what one push may expand to on disk (0 = unlimited),
	// and ScanTimeout bounds how long its frame run may take (0 = no deadline).
	// Both failures land on the scan-failure path: the gate could not evaluate
	// the push, so it is rejected under enforcement rather than waved through.
	MaxTreeBytes int64
	ScanTimeout  time.Duration
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
			tmp, err := os.MkdirTemp(d.ScanTmpDir, "afgw-")
			if err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: err.Error()}}
				continue
			}
			defer os.RemoveAll(tmp)
			if err := materializeTree(d.GitDir, r.NewRev, tmp, d.MaxTreeBytes); err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: "materialize: " + err.Error()}}
				continue
			}
			if err := overlayPolicy(d.Policy.PolicyDir, tmp); err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: "overlay: " + err.Error()}}
				continue
			}
			res, supp, err := checkWithDeadline(d.Checker, tmp, d.ScanTimeout)
			if err != nil {
				resultsByRef[r.Name] = []engine.CheckResult{{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: "check: " + err.Error()}}
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
	// Scan failure notifies even under observe: observe suppresses policy
	// rejects by design, but a gate that could not evaluate the push is an
	// outage, and in observe mode nothing else would tell the operator.
	willNotify := !dec.Accept && notifEnabled && (!d.Policy.Observe || dec.ScanFailed)

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

	// The audit log is the operator's channel ("newspaper out"), so it carries
	// the scan-failure detail the pusher-facing output withholds.
	auditMsgs := dec.Messages
	if len(dec.ScanFailures) > 0 {
		auditMsgs = append(append([]string{}, dec.Messages...), dec.ScanFailures...)
	}
	_ = AppendAudit(d.AuditPath, AuditRecord{Repo: d.Policy.Repo, Refs: refNames, RefUpdates: refs, Accept: accept, Observed: observed, Messages: auditMsgs, Findings: dec.Findings, Suppressed: suppressed, Notification: notifStatus})
	if dec.ScanFailed && d.PolicyRoot != "" {
		// Second operator channel, independent of the rail being configured:
		// /health reads these back, which is the only signal an observe-mode
		// operator gets that scanning has stopped happening.
		_ = AppendEvent(d.PolicyRoot, Event{
			Event: "scan-failed", Repo: d.Policy.Repo, OK: false,
			Payload: map[string]any{"detail": strings.Join(dec.ScanFailures, "; "), "relayed": accept},
		})
	}

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

// checkWithDeadline runs the frame pass with a wall-clock bound. Every enabled
// frame walks the staged tree itself, so a large enough push can run for
// minutes with the pusher and a receive-pack slot held open the whole time.
//
// The goroutine is abandoned rather than cancelled: Checker has no context, and
// a hook process exits moments later, so the leak lasts as long as the process
// does. A timeout is a scan failure like any other - the push is rejected under
// enforcement, because a scan that did not finish proves nothing about the code.
func checkWithDeadline(c Checker, dir string, timeout time.Duration) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	if timeout <= 0 {
		return c.Check(dir)
	}
	type outcome struct {
		res  []engine.CheckResult
		supp []engine.SuppressionLog
		err  error
	}
	done := make(chan outcome, 1) // buffered: the abandoned goroutine must not block forever
	go func() {
		res, supp, err := c.Check(dir)
		done <- outcome{res, supp, err}
	}()
	select {
	case o := <-done:
		return o.res, o.supp, o.err
	case <-time.After(timeout):
		return nil, nil, fmt.Errorf("scan exceeded %s", timeout)
	}
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
		Observed:    d.Policy.Observe, // true only on the scan-failure path; policy rejects are gated to non-observe upstream
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
		out = append(out, Suppression{Frame: l.FrameID, File: strings.TrimPrefix(l.File, prefix), Label: l.Label, Severity: l.Severity, Origin: l.Origin})
	}
	return out
}
