// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway/notification"
	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// newPreReceiveHarness wires the test scaffolding shared by the notification
// rail tests: a bare repo + one commit, an audit path, a stub upstream + an
// orchestrator pointed at it. Returns the deps, policy root, head SHA, and
// the stub so each test can flip per-repo NotificationConfig + seed PRs
// without restating the boilerplate.
func newPreReceiveHarness(t *testing.T, results []engine.CheckResult, suppressed []engine.SuppressionLog) (PreReceiveDeps, string, string, *upstream.Stub) {
	t.Helper()
	bare, sha := makeBareWithCommit(t)
	policyRoot := t.TempDir()
	// Pre-create the repo's per-repo dir so queue writes can succeed.
	if err := os.MkdirAll(filepath.Join(policyRoot, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}

	stub := upstream.NewStub()
	reg := upstream.NewRegistry()
	reg.Register("stub", stub)
	reg.RegisterHost("upstream.test", "stub")
	orch := &notification.Orchestrator{
		Upstreams:  reg,
		Webhook:    webhook.NewClient(),
		Render:     func(n notification.Notification) string { return "BODY: " + n.EventID },
		PolicyRoot: policyRoot,
	}

	deps := PreReceiveDeps{
		Policy: Policy{
			Repo:          "demo",
			UpstreamURL:   "https://upstream.test/demo",
			Enabled:       true,
			ProtectedRefs: []string{"refs/heads/main"},
			PolicyDir:     t.TempDir(),
		},
		GitDir:         bare,
		Checker:        fakeChecker{results: results, suppressed: suppressed},
		AuditPath:      t.TempDir() + "/a.log",
		Orchestrator:   orch,
		GatewayVersion: "v0.1.0-test",
		InstanceID:     "gw-test",
		PolicyRoot:     policyRoot,
	}
	return deps, policyRoot, sha, stub
}

func TestRunPreReceive_RejectWithNotification_WritesQueueRecord(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "config/key.pem:1 - PEM EC private key found"}},
		nil,
	)
	deps.NotificationConfig = &NotificationConfig{
		Enabled:      true,
		UpstreamKind: "stub",
		WebhookURL:   "", // no webhook - orchestrator inline attempt is comment-only (no PR seeded → no-op)
	}
	// Don't inject orchestrator in this test so we can assert the queue file
	// retains the record (no inline success removes it).
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 1 {
		t.Fatalf("expected reject (code 1), got %d. out=%s", code, out.String())
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, err := notification.ReadQueueRecords(queuePath)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 queued record, got %d", len(records))
	}
	rec := records[0]
	if rec.ID == "" || !strings.HasPrefix(rec.ID, "evt_") {
		t.Errorf("record ID malformed: %q", rec.ID)
	}
	if rec.Notification.Event != "push.rejected" {
		t.Errorf("Notification.Event = %q, want push.rejected", rec.Notification.Event)
	}
	if rec.Notification.Repo.Name != "demo" {
		t.Errorf("Notification.Repo.Name = %q, want demo", rec.Notification.Repo.Name)
	}
	if rec.UpstreamKind != "stub" {
		t.Errorf("UpstreamKind = %q, want stub", rec.UpstreamKind)
	}
	if len(rec.Notification.Decision.Findings) != 1 {
		t.Fatalf("Decision.Findings = %+v", rec.Notification.Decision.Findings)
	}
	f := rec.Notification.Decision.Findings[0]
	if f.FrameID != "security/x" || f.File != "config/key.pem" || f.Line != 1 {
		t.Errorf("finding parse wrong: %+v", f)
	}
}

func TestRunPreReceive_RejectWithNotificationDisabled_NoQueueRecord(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	deps.NotificationConfig = &NotificationConfig{Enabled: false}
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 1 {
		t.Fatalf("expected reject (code 1), got %d", code)
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, err := notification.ReadQueueRecords(queuePath)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("disabled notification config should leave queue empty, got %d records", len(records))
	}
}

func TestRunPreReceive_RejectWithNilNotificationConfig_NoQueueRecord(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	// NotificationConfig left nil - pre-receive must behave exactly like before this task.
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 1 {
		t.Fatalf("expected reject (code 1), got %d", code)
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, _ := notification.ReadQueueRecords(queuePath)
	if len(records) != 0 {
		t.Errorf("nil NotificationConfig should leave queue empty, got %d records", len(records))
	}
}

func TestRunPreReceive_ObserveMode_NoQueueRecord(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	deps.Policy.Observe = true
	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub"}
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 0 {
		t.Fatalf("observe mode must relay (exit 0), got %d", code)
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, _ := notification.ReadQueueRecords(queuePath)
	if len(records) != 0 {
		t.Errorf("observe mode should NOT queue notifications, got %d records", len(records))
	}
}

func TestRunPreReceive_Accept_NoQueueRecord(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t, nil, nil)
	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub"}
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 0 {
		t.Fatalf("clean push must accept (exit 0), got %d", code)
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, _ := notification.ReadQueueRecords(queuePath)
	if len(records) != 0 {
		t.Errorf("accepted push should not fire notification, got %d records", len(records))
	}
}

func TestRunPreReceive_QueueWriteFails_StillRejects(t *testing.T) {
	deps, _, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub"}
	deps.Orchestrator = nil
	// Force queue write to fail by pointing PolicyRoot at a path whose parent
	// doesn't exist AND whose grandparent is the repo's per-repo dir; the
	// AppendQueueRecord's O_CREATE call will fail on the missing parent.
	deps.PolicyRoot = filepath.Join(deps.PolicyRoot, "nonexistent", "nested")

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 1 {
		t.Fatalf("queue write failure must still reject (code 1), got %d", code)
	}
	// The queue-write failure is an operator concern (recorded as an event) and
	// naming the notification rail would reveal the gateway - it must NOT reach
	// the pusher. The reject itself still happens.
	if strings.Contains(out.String(), "notification queue write failed") {
		t.Errorf("queue-write failure must not leak to the pusher:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "push rejected") {
		t.Errorf("user-visible reject message missing from output:\n%s", out.String())
	}
}

func TestRunPreReceive_InlineDelivery_RemovesQueueRecord(t *testing.T) {
	deps, policyRoot, sha, stub := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "config/key.pem:1 - PEM key"}},
		nil,
	)
	// Seed the stub with a PR on the ref so the orchestrator goes through
	// the comment path; webhook left empty so the delivery succeeds entirely
	// on the comment-create path.
	stub.AddPR("demo", "refs/heads/main", &upstream.PullRequest{Number: 1, URL: "https://upstream.test/demo/pulls/1"})
	stub.SetPeople(1, upstream.PRPeople{})

	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub"}

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 1 {
		t.Fatalf("expected reject (code 1), got %d", code)
	}

	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	records, _ := notification.ReadQueueRecords(queuePath)
	if len(records) != 0 {
		t.Errorf("inline delivery success should drain the queue, got %d residual records", len(records))
	}
	comments := stub.Comments(1)
	if len(comments) != 1 {
		t.Errorf("inline delivery should have created exactly one comment, got %d", len(comments))
	}
}

// The observe-silence contract: an agent pushing to an observed repo must see
// output indistinguishable from a push with no gateway in front. Any
// would-have-rejected or suppression chatter contaminates the observation -
// the agent adapts to the observer instead of behaving naturally.
func TestRunPreReceive_ObserveMode_SilentToAgent(t *testing.T) {
	deps, _, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "config/key.pem:1 - PEM key"}},
		[]engine.SuppressionLog{{FrameID: "security/y", File: "a.go", Label: "known-fixture"}},
	)
	deps.Policy.Observe = true
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 0 {
		t.Fatalf("observe mode must relay (exit 0), got %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("observe mode must write NOTHING to the client; got:\n%s", out.String())
	}

	recs := tailParse(deps.AuditPath, 10)
	if len(recs) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(recs))
	}
	rec := recs[0]
	if !rec.Observed || !rec.Accept {
		t.Errorf("audit record should carry Observed=true Accept=true, got Observed=%v Accept=%v", rec.Observed, rec.Accept)
	}
	if len(rec.Messages) == 0 {
		t.Error("audit record should retain the would-have-rejected messages for the operator")
	}
	if len(rec.Suppressed) != 1 {
		t.Errorf("audit record should retain suppressions, got %d", len(rec.Suppressed))
	}
}

func TestRunPreReceive_AcceptWithSuppressions_SilentToAgent(t *testing.T) {
	deps, _, sha, _ := newPreReceiveHarness(t,
		nil, // no open findings - whitelist suppressed everything
		[]engine.SuppressionLog{{FrameID: "security/x", File: "a.go", Label: "fixture"}, {FrameID: "security/x", File: "b.go", Label: "fixture"}},
	)
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 0 {
		t.Fatalf("clean push must accept (exit 0), got %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("accepted push must write NOTHING to the client (suppression count included); got:\n%s", out.String())
	}

	recs := tailParse(deps.AuditPath, 10)
	if len(recs) != 1 || len(recs[0].Suppressed) != 2 {
		t.Fatalf("audit record should retain 2 suppressions, got %+v", recs)
	}
}

func TestRunPreReceive_Reject_ShowsSuppressionCountAndFindings(t *testing.T) {
	deps, _, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "config/key.pem:1 - PEM key"}},
		[]engine.SuppressionLog{{FrameID: "security/y", File: "a.go", Label: "fixture"}},
	)
	deps.Orchestrator = nil

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 1 {
		t.Fatalf("expected reject (code 1), got %d", code)
	}
	s := out.String()
	// Camouflage: the reject reads like a vanilla git host's policy reject -
	// the findings the dev needs, but no branding, no whitelist-suppression
	// count (operator-only), no gateway/relay language.
	if !strings.Contains(s, "push rejected by repository policy") {
		t.Errorf("reject must show a generic policy reason:\n%s", s)
	}
	if !strings.Contains(s, "PEM key") {
		t.Errorf("reject must surface the findings the dev needs to fix:\n%s", s)
	}
	if strings.Contains(s, "suppressed by whitelist") {
		t.Errorf("whitelist suppression count must NOT leak to the pusher:\n%s", s)
	}
	low := strings.ToLower(s)
	for _, leak := range []string{"nimblegate", "gateway", "relay", "upstream"} {
		if strings.Contains(low, leak) {
			t.Errorf("reject output leaked %q (must look like a vanilla host):\n%s", leak, s)
		}
	}
}

// TestEnqueueResolutions_purgesPendingRejects is the end-to-end wiring for the
// #3 fix: when a clean push resolves a ref, enqueueResolutions appends the
// resolution AND drops any reject record still pending for that ref, so a stale
// reject can't deliver afterward and flip the ✅ comment back to ⛔.
func TestEnqueueResolutions_purgesPendingRejects(t *testing.T) {
	policyRoot := t.TempDir()
	repo := "foo"
	if err := os.MkdirAll(filepath.Join(policyRoot, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(policyRoot, repo, "pr-comment-queue.jsonl")
	ref := "refs/heads/x"
	// A reject for the ref is still pending (e.g. stuck on backoff).
	_ = notification.AppendQueueRecord(queuePath, notification.QueueRecord{
		ID:           "stale-reject",
		Notification: notification.Notification{Event: "push.rejected", Push: notification.PushInfo{Refs: []notification.RefInfo{{Name: ref}}}},
	})

	d := PreReceiveDeps{PolicyRoot: policyRoot, Policy: Policy{Repo: repo}}
	resRec := notification.QueueRecord{
		ID:           "resolution",
		Notification: notification.Notification{Event: "push.resolved", Push: notification.PushInfo{Refs: []notification.RefInfo{{Name: ref}}}},
	}
	enqueueResolutions(d, []notification.QueueRecord{resRec}, []int{1})

	ids := map[string]bool{}
	got, _ := notification.ReadQueueRecords(queuePath)
	for _, r := range got {
		ids[r.ID] = true
	}
	if ids["stale-reject"] {
		t.Error("stale reject for the resolved ref must be purged by enqueueResolutions")
	}
	if !ids["resolution"] {
		t.Error("the resolution record must remain in the queue")
	}
}
