// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway/notification"
	"nimblegate/internal/gateway/notification/render"
	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// e2eWebhook captures POSTs the gateway sends to a configured webhook URL.
// It buffers every (body, signature, content-type) triple so tests can assert
// on count + payload + HMAC signature without racing on a single overwrite.
type e2eWebhook struct {
	mu     sync.Mutex
	bodies [][]byte
	sigs   []string
	cts    []string
}

func (w *e2eWebhook) record(body []byte, sig, ct string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bodies = append(w.bodies, body)
	w.sigs = append(w.sigs, sig)
	w.cts = append(w.cts, ct)
}

func (w *e2eWebhook) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.bodies)
}

func (w *e2eWebhook) last() ([]byte, string, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(w.bodies)
	if n == 0 {
		return nil, "", ""
	}
	return w.bodies[n-1], w.sigs[n-1], w.cts[n-1]
}

// newE2EWebhookServer returns an httptest.Server that captures POSTs into got.
func newE2EWebhookServer(t *testing.T, got *e2eWebhook) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.record(body, r.Header.Get("X-Nimblegate-Signature"), r.Header.Get("Content-Type"))
		w.WriteHeader(200)
	}))
}

// e2eHarness wires the full notification rail with real components - the
// stub upstream adapter, a registry, a render-backed orchestrator, a
// per-test policyRoot, and a webhook httptest.Server. The harness owns the
// non-disposable bits so each scenario can build its PreReceiveDeps off it.
type e2eHarness struct {
	policyRoot string
	stub       *upstream.Stub
	reg        *upstream.Registry
	orch       *notification.Orchestrator
	webhookSrv *httptest.Server
	webhook    *e2eWebhook
	bare       string
	sha        string
}

// newE2EHarness builds the harness for repo "demo" on host "upstream.test".
// The bare repo carries one commit so RunPreReceive can materialize a tree.
func newE2EHarness(t *testing.T) *e2eHarness {
	t.Helper()
	bare, sha := makeBareWithCommit(t)
	policyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(policyRoot, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	stub := upstream.NewStub()
	reg := upstream.NewRegistry()
	reg.Register("stub", stub)
	reg.RegisterHost("upstream.test", "stub")

	got := &e2eWebhook{}
	srv := newE2EWebhookServer(t, got)
	t.Cleanup(srv.Close)

	orch := &notification.Orchestrator{
		Upstreams:  reg,
		Webhook:    webhook.NewClient(),
		Render:     render.Comment, // real renderer
		PolicyRoot: policyRoot,
	}
	return &e2eHarness{
		policyRoot: policyRoot,
		stub:       stub,
		reg:        reg,
		orch:       orch,
		webhookSrv: srv,
		webhook:    got,
		bare:       bare,
		sha:        sha,
	}
}

// blockResults returns a single-finding CheckResult slice that trips a BLOCK.
// The reason carries a "file:line - label" prefix so notification.Build can
// parse File + Line out of it (exercises the real parse path).
func blockResults(frameID, reason string) []engine.CheckResult {
	return []engine.CheckResult{{
		FrameID: frameID,
		Outcome: engine.OutcomeBlock,
		Reason:  reason,
	}}
}

// runReject runs RunPreReceive with the harness's deps + the given checker
// results. Returns the exit code + captured stdout.
func (h *e2eHarness) runReject(t *testing.T, results []engine.CheckResult, ncfg *NotificationConfig, withOrchestrator bool) (int, string) {
	t.Helper()
	deps := PreReceiveDeps{
		Policy: Policy{
			Repo:          "demo",
			UpstreamURL:   "https://upstream.test/demo",
			Enabled:       true,
			ProtectedRefs: []string{"refs/heads/main"},
			PolicyDir:     t.TempDir(),
		},
		GitDir:             h.bare,
		Checker:            fakeChecker{results: results},
		AuditPath:          filepath.Join(h.policyRoot, "demo", "audit.log"),
		NotificationConfig: ncfg,
		GatewayVersion:     "v0.1.0-e2e",
		InstanceID:         "gw-e2e",
		PolicyRoot:         h.policyRoot,
	}
	if withOrchestrator {
		deps.Orchestrator = h.orch
	}
	stdin := strings.NewReader(zeroRev + " " + h.sha + " refs/heads/main\n")
	var out bytes.Buffer
	code := RunPreReceive(deps, stdin, &out)
	return code, out.String()
}

// readAuditRecords reads every JSON line from the harness's audit log.
func (h *e2eHarness) readAuditRecords(t *testing.T) []AuditRecord {
	t.Helper()
	path := filepath.Join(h.policyRoot, "demo", "audit.log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open audit %s: %v", path, err)
	}
	defer f.Close()
	var out []AuditRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("audit line not JSON: %v\n%s", err, sc.Text())
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan audit: %v", err)
	}
	return out
}

func (h *e2eHarness) queuePath() string {
	return filepath.Join(h.policyRoot, "demo", "pr-comment-queue.jsonl")
}

// hmacAuth is the standard auth used across the E2E scenarios so the webhook
// signature can be verified end-to-end.
func hmacAuth(secret string) notification.WebhookAuth {
	return notification.WebhookAuth{Mode: "hmac", Secret: secret}
}

// TestE2E_NotificationRail_RealPushTripsBlock is scenario 1: a push that
// trips a BLOCK finding writes BOTH an audit record AND a queue record. No
// orchestrator is injected, so the queue record is left for the daemon to
// drain - exercising the durability anchor of spec §3.4.
func TestE2E_NotificationRail_RealPushTripsBlock(t *testing.T) {
	h := newE2EHarness(t)

	code, out := h.runReject(t,
		blockResults("security/no-private-keys-in-repo", "config/key.pem:1 - PEM EC private key found"),
		&NotificationConfig{
			Enabled:      true,
			UpstreamKind: "stub",
			WebhookURL:   h.webhookSrv.URL,
			WebhookAuth:  hmacAuth("scenario-1"),
		},
		false, // no orchestrator → no inline delivery
	)
	if code != 1 {
		t.Fatalf("BLOCK push must reject (code 1), got %d. out=%s", code, out)
	}

	// Audit record: one row, rejected, carries the BLOCK finding.
	audit := h.readAuditRecords(t)
	if len(audit) != 1 {
		t.Fatalf("want 1 audit record, got %d: %+v", len(audit), audit)
	}
	rec := audit[0]
	if rec.Accept {
		t.Errorf("audit record should be Accept=false (rejected)")
	}
	if rec.Observed {
		t.Errorf("non-observe reject should be Observed=false")
	}
	if len(rec.Findings) != 1 || rec.Findings[0].ID != "security/no-private-keys-in-repo" {
		t.Errorf("audit findings = %+v, want one no-private-keys BLOCK", rec.Findings)
	}

	// Queue record: one row, parsed file/line, ID matches Notification.EventID.
	records, err := notification.ReadQueueRecords(h.queuePath())
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 queued record, got %d", len(records))
	}
	q := records[0]
	if q.ID == "" || q.ID != q.Notification.EventID {
		t.Errorf("queue record ID must mirror Notification.EventID: %q vs %q", q.ID, q.Notification.EventID)
	}
	if q.Notification.Event != "push.rejected" {
		t.Errorf("Event = %q, want push.rejected", q.Notification.Event)
	}
	if q.UpstreamKind != "stub" {
		t.Errorf("UpstreamKind = %q, want stub", q.UpstreamKind)
	}
	if q.WebhookURL != h.webhookSrv.URL {
		t.Errorf("WebhookURL not preserved on queue record: %q", q.WebhookURL)
	}
	if len(q.Notification.Decision.Findings) != 1 {
		t.Fatalf("queued Decision.Findings = %+v", q.Notification.Decision.Findings)
	}
	f := q.Notification.Decision.Findings[0]
	if f.File != "config/key.pem" || f.Line != 1 {
		t.Errorf("finding file/line parse wrong: %+v", f)
	}
}

// TestE2E_NotificationRail_DaemonDrainsOlderRecord is scenario 2: a queued
// record older than InlineRaceGap is picked up by Daemon.PollOnce and
// delivered through the orchestrator. CreateComment is called on the stub
// adapter, the webhook httptest.Server receives the POST + valid HMAC, and
// the queue is empty afterwards.
func TestE2E_NotificationRail_DaemonDrainsOlderRecord(t *testing.T) {
	h := newE2EHarness(t)

	// Pre-seed a PR so the orchestrator goes through the comment path.
	pr := &upstream.PullRequest{Number: 42, URL: "https://upstream.test/demo/pulls/42"}
	h.stub.AddPR("demo", "refs/heads/main", pr)
	h.stub.SetPeople(42, upstream.PRPeople{Assignees: []string{"alice"}})

	// 1) Generate the queue record via a real pre-receive run - no
	//    orchestrator → record stays in the queue for the daemon to drain.
	code, _ := h.runReject(t,
		blockResults("security/no-hardcoded-credentials", "src/cfg.go:10 - API key"),
		&NotificationConfig{
			Enabled:      true,
			UpstreamKind: "stub",
			WebhookURL:   h.webhookSrv.URL,
			WebhookAuth:  hmacAuth("scenario-2"),
		},
		false,
	)
	if code != 1 {
		t.Fatalf("reject expected, got code %d", code)
	}
	queued, _ := notification.ReadQueueRecords(h.queuePath())
	if len(queued) != 1 {
		t.Fatalf("want 1 record in queue before daemon poll, got %d", len(queued))
	}
	queuedAt := queued[0].QueuedAt

	// 2) Build a daemon whose "now" is 31s after queuedAt - older than the
	//    30s InlineRaceGap, so the record is eligible for drain. Inject the
	//    same orchestrator as inline would use.
	d := &notification.Daemon{
		PolicyRoot:   h.policyRoot,
		Orchestrator: h.orch,
		Config:       notification.DefaultDaemonConfig(),
		Now:          func() time.Time { return queuedAt.Add(31 * time.Second) },
	}
	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	// 3) Adapter saw exactly one CreateComment call.
	comments := h.stub.Comments(42)
	if len(comments) != 1 {
		t.Fatalf("want 1 comment created via CreateComment, got %d", len(comments))
	}
	if !strings.Contains(comments[0].Body, render.MarkerStart) {
		t.Errorf("rendered body missing nimblegate-data marker (real renderer wasn't used?)")
	}

	// 4) Webhook server received the POST + valid HMAC signature.
	if h.webhook.count() != 1 {
		t.Fatalf("webhook should have received 1 POST, got %d", h.webhook.count())
	}
	body, sig, ct := h.webhook.last()
	if ct != "application/json" {
		t.Errorf("webhook Content-Type = %q, want application/json", ct)
	}
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature must be sha256-prefixed, got %q", sig)
	}
	if !webhook.VerifyHMAC("scenario-2", body, strings.TrimPrefix(sig, "sha256=")) {
		t.Errorf("HMAC signature does not verify against captured body")
	}

	// 5) Queue drained.
	residual, _ := notification.ReadQueueRecords(h.queuePath())
	if len(residual) != 0 {
		t.Errorf("queue should be drained after successful delivery, got %d residual", len(residual))
	}
}

// TestE2E_NotificationRail_StickyUpdateOnSecondReject is scenario 3: two
// rejects on the same PR cause adapter.UpdateComment (not CreateComment) to
// be called on the second push. PRState's StickyComment.ID is populated by
// the first delivery and reused by the second.
func TestE2E_NotificationRail_StickyUpdateOnSecondReject(t *testing.T) {
	h := newE2EHarness(t)

	// PR seeded; PRState needs to exist beforehand so the orchestrator can
	// persist the sticky ID onto it (per orchestrator.go - it only writes if
	// ReadPRState returns non-nil).
	pr := &upstream.PullRequest{Number: 7, URL: "https://upstream.test/demo/pulls/7"}
	h.stub.AddPR("demo", "refs/heads/main", pr)
	h.stub.SetPeople(7, upstream.PRPeople{})
	if err := notification.WritePRState(h.policyRoot, "demo", 7, notification.PRState{
		PRNumber: 7,
		Repo:     "demo",
		Ref:      "refs/heads/main",
		Loop:     notification.LoopCounters{AttemptCount: 1, MaxAttempts: 5},
	}); err != nil {
		t.Fatalf("seed PRState: %v", err)
	}

	ncfg := &NotificationConfig{
		Enabled:      true,
		UpstreamKind: "stub",
		WebhookURL:   h.webhookSrv.URL,
		WebhookAuth:  hmacAuth("scenario-3"),
	}

	// First reject (with orchestrator) → inline delivery creates the comment
	// + writes StickyComment.ID into PRState.
	if code, _ := h.runReject(t,
		blockResults("security/no-private-keys-in-repo", "k.pem:1 - PEM"),
		ncfg, true,
	); code != 1 {
		t.Fatalf("first reject expected code 1, got %d", code)
	}

	commentsAfterFirst := h.stub.Comments(7)
	if len(commentsAfterFirst) != 1 {
		t.Fatalf("after first reject want 1 comment (CreateComment), got %d", len(commentsAfterFirst))
	}
	firstCommentID := commentsAfterFirst[0].ID
	firstBody := commentsAfterFirst[0].Body

	state, err := notification.ReadPRState(h.policyRoot, "demo", 7)
	if err != nil || state == nil {
		t.Fatalf("PRState missing after first delivery: state=%+v err=%v", state, err)
	}
	if state.StickyComment.ID != firstCommentID {
		t.Fatalf("StickyComment.ID = %q, want first comment ID %q", state.StickyComment.ID, firstCommentID)
	}

	// Second reject (with orchestrator) - different finding so the rendered
	// body changes. The orchestrator must locate the sticky via PRState's
	// StickyCommentID and call UpdateComment, NOT CreateComment.
	// IMPORTANT: pre-receive needs to write the QueueRecord.State.StickyCommentID
	// from PRState, but Task 22's wiring may not yet copy it. The orchestrator
	// also falls back to ScanForMarker, so the existing nimblegate-data marker
	// in the first body guarantees we still update, not create.
	if code, _ := h.runReject(t,
		blockResults("security/no-hardcoded-credentials", "src/secrets.go:5 - token"),
		ncfg, true,
	); code != 1 {
		t.Fatalf("second reject expected code 1, got %d", code)
	}

	commentsAfterSecond := h.stub.Comments(7)
	if len(commentsAfterSecond) != 1 {
		t.Fatalf("after second reject want STILL 1 comment (UpdateComment, not CreateComment), got %d", len(commentsAfterSecond))
	}
	if commentsAfterSecond[0].ID != firstCommentID {
		t.Errorf("sticky comment ID changed (%q→%q) - orchestrator should have UPDATED, not created a new one",
			firstCommentID, commentsAfterSecond[0].ID)
	}
	if commentsAfterSecond[0].Body == firstBody {
		t.Errorf("comment body should have been rewritten on update")
	}
	if !strings.Contains(commentsAfterSecond[0].Body, "no-hardcoded-credentials") {
		t.Errorf("updated body should reflect the second finding; got:\n%s", commentsAfterSecond[0].Body)
	}

	// PRState's StickyComment.ID still points at the same comment.
	state2, _ := notification.ReadPRState(h.policyRoot, "demo", 7)
	if state2 == nil || state2.StickyComment.ID != firstCommentID {
		t.Errorf("PRState.StickyComment.ID drifted: %+v", state2)
	}
}

// TestE2E_NotificationRail_WebhookPayloadMatchesQueueRecord is scenario 4:
// the JSON the webhook server receives is byte-identical to the queue
// record's Notification field, and the HMAC signature verifies against it.
// This is the contract between the rail's two channels - same payload,
// different transport.
func TestE2E_NotificationRail_WebhookPayloadMatchesQueueRecord(t *testing.T) {
	h := newE2EHarness(t)

	// No PR seeded → orchestrator skips comment work, exercises pure webhook
	// path. Pre-receive uses orchestrator inline, so the queue record gets
	// removed on success - capture it BEFORE the orchestrator wipes it.
	ncfg := &NotificationConfig{
		Enabled:      true,
		UpstreamKind: "stub",
		WebhookURL:   h.webhookSrv.URL,
		WebhookAuth:  hmacAuth("scenario-4"),
	}

	// Run pre-receive WITHOUT orchestrator first to land the record on disk,
	// then re-read it before invoking the orchestrator. This is the same
	// shape the daemon path produces - and the queue record's Notification
	// field is exactly what gets marshalled to the webhook.
	if code, _ := h.runReject(t,
		blockResults("security/x", "f.go:2 - bad"),
		ncfg, false,
	); code != 1 {
		t.Fatalf("reject expected, got code")
	}
	queued, _ := notification.ReadQueueRecords(h.queuePath())
	if len(queued) != 1 {
		t.Fatalf("want 1 queued record, got %d", len(queued))
	}
	rec := queued[0]

	// Deliver the queued record through the orchestrator directly.
	if err := h.orch.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	// Webhook received exactly one POST.
	if h.webhook.count() != 1 {
		t.Fatalf("webhook count = %d, want 1", h.webhook.count())
	}
	body, sig, _ := h.webhook.last()

	// The webhook body must be the JSON marshalling of the queued record's
	// Notification field (the orchestrator's payload contract).
	wantPayload, err := json.Marshal(rec.Notification)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(body, wantPayload) {
		t.Errorf("webhook body != json(queue.Notification).\n got:  %s\n want: %s", body, wantPayload)
	}

	// HMAC over the captured body verifies with the configured secret.
	if !webhook.VerifyHMAC("scenario-4", body, strings.TrimPrefix(sig, "sha256=")) {
		t.Errorf("HMAC verification failed for webhook body")
	}

	// EventID embedded in the JSON matches the queue record ID - guarantees
	// the webhook receiver can dedup on the same key the gateway uses.
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse webhook JSON: %v", err)
	}
	if parsed["event_id"] != rec.ID {
		t.Errorf("webhook event_id = %v, queue ID = %q (must match)", parsed["event_id"], rec.ID)
	}
}

// TestE2E_NotificationRail_NoPRFallback is scenario 5: a push on a ref with
// no open PR causes the webhook to fire BUT no CreateComment call on the
// adapter. The rail's two channels are independent - webhook still gets
// notified even when the comment side has nothing to do.
func TestE2E_NotificationRail_NoPRFallback(t *testing.T) {
	h := newE2EHarness(t)
	// Deliberately do NOT seed a PR - adapter.FindPRForRef returns nil.

	code, _ := h.runReject(t,
		blockResults("security/x", "x.go:1 - boom"),
		&NotificationConfig{
			Enabled:      true,
			UpstreamKind: "stub",
			WebhookURL:   h.webhookSrv.URL,
			WebhookAuth:  hmacAuth("scenario-5"),
		},
		true, // orchestrator wired so inline attempt runs
	)
	if code != 1 {
		t.Fatalf("reject expected, got %d", code)
	}

	// Comment side: no PRs registered for any number - Comments(0) is empty,
	// and the adapter never ran CreateComment.
	for _, prNum := range []int{0, 1, 42} {
		if cs := h.stub.Comments(prNum); len(cs) != 0 {
			t.Errorf("no-PR fallback created comments on PR %d: %+v", prNum, cs)
		}
	}

	// Webhook side: POST landed, signature valid.
	if h.webhook.count() != 1 {
		t.Fatalf("webhook should fire even with no PR; got %d POSTs", h.webhook.count())
	}
	body, sig, ct := h.webhook.last()
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !webhook.VerifyHMAC("scenario-5", body, strings.TrimPrefix(sig, "sha256=")) {
		t.Errorf("HMAC verification failed in no-PR fallback")
	}

	// The queue must be drained - inline DeliverOne succeeded (no PR is not
	// an error when the webhook lands).
	residual, _ := notification.ReadQueueRecords(h.queuePath())
	if len(residual) != 0 {
		t.Errorf("queue should be drained after no-PR success, got %d residual records", len(residual))
	}
}
