// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// captured holds the webhook server's record of the last POST it received.
type captured struct {
	body []byte
	sig  string
	ct   string
}

// newWebhookServer returns an httptest.Server that records the incoming POST
// body + signature header + content-type into got, then returns status.
func newWebhookServer(t *testing.T, status int, got *captured) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.sig = r.Header.Get("X-Nimblegate-Signature")
		got.ct = r.Header.Get("Content-Type")
		w.WriteHeader(status)
	}))
}

// buildOrchestrator returns the (registry, orchestrator) pair seeded with stub
// for upstreamURL.
func buildOrchestrator(stub *upstream.Stub, upstreamHost, policyRoot string) (*upstream.Registry, *Orchestrator) {
	reg := upstream.NewRegistry()
	reg.Register("stub", stub)
	reg.RegisterHost(upstreamHost, "stub")
	o := &Orchestrator{
		Upstreams:  reg,
		Webhook:    webhook.NewClient(),
		Render:     func(n Notification) string { return "BODY: " + n.EventID },
		PolicyRoot: policyRoot,
	}
	return reg, o
}

// baseRec returns a QueueRecord with the minimum populated fields for
// driving DeliverOne. Webhook URL is left empty; callers set it.
func baseRec(eventID, repoName, upstreamURL, ref string) QueueRecord {
	return QueueRecord{
		ID:           eventID,
		UpstreamKind: "stub",
		Notification: Notification{
			SchemaVersion: SchemaVersion,
			EventID:       eventID,
			Event:         "push.rejected",
			Repo:          RepoInfo{Name: repoName, UpstreamURL: upstreamURL},
			Push:          PushInfo{Refs: []RefInfo{{Name: ref}}},
		},
	}
}

func TestDeliverOne_AdvancesLoopAttemptCount(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 7, URL: "https://upstream.test/r/pulls/7"}
	stub.AddPR("gw-test", "refs/heads/fix-demo", pr)

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	deliver := func(id string) {
		rec := baseRec(id, "gw-test", "https://upstream.test/gw-test", "refs/heads/fix-demo")
		// A real loop config is what flips DeliverOne into loop-tracking mode.
		rec.LoopConfig = LoopConfig{MaxAttempts: 5, DefaultMention: "@nimblegate-bot"}
		if err := o.DeliverOne(context.Background(), rec); err != nil {
			t.Fatalf("DeliverOne(%s): %v", id, err)
		}
	}

	deliver("evt1")
	s1, _ := ReadPRState(tmpRoot, "gw-test", 7)
	if s1 == nil || s1.Loop.AttemptCount != 1 {
		t.Fatalf("after first reject, AttemptCount = %+v, want 1", s1)
	}
	if s1.Mention.CurrentBot != "@nimblegate-bot" {
		t.Errorf("CurrentBot = %q, want @nimblegate-bot", s1.Mention.CurrentBot)
	}
	if s1.Loop.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", s1.Loop.MaxAttempts)
	}

	deliver("evt2")
	s2, _ := ReadPRState(tmpRoot, "gw-test", 7)
	if s2 == nil || s2.Loop.AttemptCount != 2 {
		t.Fatalf("after second reject, AttemptCount = %+v, want 2", s2)
	}
}

func TestDeliverOne_Resolved_UpdatesExistingComment(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 9, URL: "https://upstream.test/repo/pulls/9"}
	stub.AddPR("repo", "refs/heads/fix-demo", pr)

	// The prior reject left a sticky comment; resolution updates it in place.
	stub.AddComment(9, &upstream.Comment{ID: "comment_reject", Body: "⛔ old reject body"})

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_resolved", "repo", "https://upstream.test/repo", "refs/heads/fix-demo")
	rec.Notification.Event = "push.resolved"
	rec.State = QueueRecordState{PRNumber: 9, StickyCommentID: "comment_reject"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne resolved: %v", err)
	}
	comments := stub.Comments(9)
	if len(comments) != 1 {
		t.Fatalf("expected the sticky comment updated in place, got %d comments", len(comments))
	}
	if !strings.Contains(comments[0].Body, "BODY: evt_resolved") {
		t.Errorf("resolved comment not updated in place: %q", comments[0].Body)
	}
}

func TestDeliverOne_NoPriorSticky_CreatesAndPersistsID(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 42, URL: "https://upstream.test/test-repo/pulls/42"}
	stub.AddPR("test-repo", "refs/heads/main", pr)
	stub.SetPeople(42, upstream.PRPeople{Assignees: []string{"alice"}})

	tmpRoot := t.TempDir()
	if err := WritePRState(tmpRoot, "test-repo", 42, PRState{PRNumber: 42, Repo: "test-repo", Loop: LoopCounters{AttemptCount: 1, MaxAttempts: 5}}); err != nil {
		t.Fatalf("seed PRState: %v", err)
	}

	var got captured
	srv := newWebhookServer(t, 200, &got)
	defer srv.Close()

	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)
	rec := baseRec("evt_create", "test-repo", "https://upstream.test/test-repo", "refs/heads/main")
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "hmac", Secret: "test-secret"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	// (a) CreateComment was called with the rendered body.
	comments := stub.Comments(42)
	if len(comments) != 1 {
		t.Fatalf("want 1 comment created, got %d", len(comments))
	}
	if !strings.Contains(comments[0].Body, "BODY: evt_create") {
		t.Errorf("comment body missing render output: %q", comments[0].Body)
	}

	// (b) PRState picked up the new sticky comment ID.
	state, err := ReadPRState(tmpRoot, "test-repo", 42)
	if err != nil {
		t.Fatalf("ReadPRState: %v", err)
	}
	if state == nil {
		t.Fatalf("PRState should exist after delivery")
	}
	if state.StickyComment.ID != comments[0].ID {
		t.Errorf("StickyComment.ID = %q, want %q", state.StickyComment.ID, comments[0].ID)
	}

	// (c) Webhook fired with the JSON payload + HMAC signature.
	if got.ct != "application/json" {
		t.Errorf("webhook Content-Type = %q, want application/json", got.ct)
	}
	if got.sig == "" {
		t.Errorf("webhook missing X-Nimblegate-Signature header")
	}
	// Verify the signature is valid against the captured body.
	if !strings.HasPrefix(got.sig, "sha256=") {
		t.Errorf("signature prefix wrong: %q", got.sig)
	}
	if !webhook.VerifyHMAC("test-secret", got.body, strings.TrimPrefix(got.sig, "sha256=")) {
		t.Errorf("signature does not verify against captured body")
	}
}

func TestDeliverOne_PriorStickyByID_Updates(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 7, URL: "https://upstream.test/repo/pulls/7"}
	stub.AddPR("repo", "refs/heads/feature", pr)
	stub.SetPeople(7, upstream.PRPeople{})

	// Seed an existing comment with a known ID.
	existing := &upstream.Comment{ID: "comment_known", Body: "old body"}
	stub.AddComment(7, existing)

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_update", "repo", "https://upstream.test/repo", "refs/heads/feature")
	rec.State = QueueRecordState{PRNumber: 7, StickyCommentID: "comment_known"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	comments := stub.Comments(7)
	if len(comments) != 1 {
		t.Fatalf("expected exactly 1 (existing) comment, got %d", len(comments))
	}
	if !strings.Contains(comments[0].Body, "BODY: evt_update") {
		t.Errorf("existing comment body not updated: %q", comments[0].Body)
	}
}

func TestDeliverOne_StaleStickyID_FallsBackToMarkerScan(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 11, URL: "https://upstream.test/repo/pulls/11"}
	stub.AddPR("repo", "refs/heads/feature", pr)
	stub.SetPeople(11, upstream.PRPeople{})

	// Seed an existing comment carrying the nimblegate marker, but the
	// state's StickyCommentID points at a non-existent ID.
	existing := &upstream.Comment{
		ID:   "comment_real",
		Body: "stale render\n<!-- nimblegate-data:\n{}\n-->",
	}
	stub.AddComment(11, existing)

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_fallback", "repo", "https://upstream.test/repo", "refs/heads/feature")
	rec.State = QueueRecordState{PRNumber: 11, StickyCommentID: "comment_does_not_exist"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	comments := stub.Comments(11)
	if len(comments) != 1 {
		t.Fatalf("expected exactly 1 comment (existing one updated), got %d", len(comments))
	}
	if !strings.Contains(comments[0].Body, "BODY: evt_fallback") {
		t.Errorf("marker-scanned comment body not updated: %q", comments[0].Body)
	}
}

func TestDeliverOne_NoStickyAnywhere_Creates(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 99, URL: "https://upstream.test/repo/pulls/99"}
	stub.AddPR("repo", "refs/heads/main", pr)
	stub.SetPeople(99, upstream.PRPeople{})

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_new", "repo", "https://upstream.test/repo", "refs/heads/main")
	// No State.StickyCommentID; no pre-existing comment seeded.

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}
	comments := stub.Comments(99)
	if len(comments) != 1 {
		t.Fatalf("want 1 comment created, got %d", len(comments))
	}
	if !strings.Contains(comments[0].Body, "BODY: evt_new") {
		t.Errorf("comment body missing render: %q", comments[0].Body)
	}
}

func TestDeliverOne_NoPR_WebhookStillFires(t *testing.T) {
	stub := upstream.NewStub()
	// Note: no AddPR - adapter.FindPRForRef returns nil.

	var got captured
	srv := newWebhookServer(t, 200, &got)
	defer srv.Close()

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_no_pr", "repo", "https://upstream.test/repo", "refs/heads/orphan")
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "hmac", Secret: "s"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	// No PR → no comment work.
	if len(stub.Comments(0)) != 0 {
		t.Errorf("no comment should have been created when no PR")
	}
	// Webhook still fired.
	if len(got.body) == 0 {
		t.Errorf("webhook should have fired even with no PR")
	}
	if got.sig == "" {
		t.Errorf("webhook should have HMAC signature")
	}
}

func TestDeliverOne_NoPRNoWebhook_IsNoop(t *testing.T) {
	stub := upstream.NewStub()

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_nothing", "repo", "https://upstream.test/repo", "refs/heads/orphan")
	// No webhook URL configured, no PR. Should still return nil.

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne with no PR + no webhook should be a no-op, got %v", err)
	}
}

func TestDeliverOne_WebhookPayloadAndSignature(t *testing.T) {
	stub := upstream.NewStub()

	var got captured
	srv := newWebhookServer(t, 200, &got)
	defer srv.Close()

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_sig", "repo", "https://upstream.test/repo", "refs/heads/main")
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "hmac", Secret: "the-secret"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	// Payload must be the JSON-marshalled Notification.
	wantPayload, _ := json.Marshal(rec.Notification)
	if string(got.body) != string(wantPayload) {
		t.Errorf("webhook payload mismatch.\n got:  %s\n want: %s", got.body, wantPayload)
	}
	wantSig := "sha256=" + webhook.SignHMAC("the-secret", wantPayload)
	if got.sig != wantSig {
		t.Errorf("webhook signature mismatch.\n got:  %s\n want: %s", got.sig, wantSig)
	}
}

func TestDeliverOne_PRPeopleAugmentation(t *testing.T) {
	stub := upstream.NewStub()
	pr := &upstream.PullRequest{Number: 5, URL: "https://upstream.test/repo/pulls/5"}
	stub.AddPR("repo", "refs/heads/feature", pr)
	stub.SetPeople(5, upstream.PRPeople{
		Assignees: []string{"alice", "bob"},
		Reviewers: []string{"carol", "alice"}, // duplicate to test dedup
	})

	tmpRoot := t.TempDir()

	// Capture the notification at render time so we can inspect the
	// augmented assignees/reviewers + mention humans.
	var rendered Notification
	reg := upstream.NewRegistry()
	reg.Register("stub", stub)
	reg.RegisterHost("upstream.test", "stub")
	o := &Orchestrator{
		Upstreams: reg,
		Webhook:   webhook.NewClient(),
		Render: func(n Notification) string {
			rendered = n
			return "body"
		},
		PolicyRoot: tmpRoot,
	}

	rec := baseRec("evt_people", "repo", "https://upstream.test/repo", "refs/heads/feature")
	rec.Notification.Mention = &MentionInfo{CurrentBot: "@nimbot"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatalf("DeliverOne: %v", err)
	}

	if rendered.Push.PR == nil {
		t.Fatalf("Push.PR should be populated post-augmentation")
	}
	if rendered.Push.PR.Number != 5 {
		t.Errorf("PR.Number = %d, want 5", rendered.Push.PR.Number)
	}
	if len(rendered.Push.PR.Assignees) != 2 || rendered.Push.PR.Assignees[0] != "alice" {
		t.Errorf("Push.PR.Assignees = %v, want [alice bob]", rendered.Push.PR.Assignees)
	}
	if len(rendered.Push.PR.Reviewers) != 2 {
		t.Errorf("Push.PR.Reviewers = %v, want [carol alice]", rendered.Push.PR.Reviewers)
	}

	// AutoTaggedHumans should be deduped against @nimbot - and the duplicate
	// "alice" between assignees/reviewers should appear once.
	wantHumans := []string{"@alice", "@bob", "@carol"}
	if !equalSlices(rendered.Mention.AutoTaggedHumans, wantHumans) {
		t.Errorf("AutoTaggedHumans = %v, want %v", rendered.Mention.AutoTaggedHumans, wantHumans)
	}
}

func TestDeliverOne_WebhookTransientPropagates(t *testing.T) {
	stub := upstream.NewStub()
	// No PR seeded - skips comment work, exercising only webhook path.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tmpRoot := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", tmpRoot)

	rec := baseRec("evt_503", "repo", "https://upstream.test/repo", "refs/heads/main")
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}

	err := o.DeliverOne(context.Background(), rec)
	if err == nil {
		t.Fatalf("DeliverOne should error on webhook 503")
	}
	if !errors.Is(err, upstream.ErrTransient) {
		t.Errorf("error should wrap upstream.ErrTransient; got %v", err)
	}
}

func TestDeliverOne_UnknownUpstreamHost_ErrPermanent(t *testing.T) {
	stub := upstream.NewStub()
	tmpRoot := t.TempDir()
	// Register stub under a DIFFERENT host than the record uses.
	_, o := buildOrchestrator(stub, "registered.test", tmpRoot)

	rec := baseRec("evt_bad_host", "repo", "https://unregistered.test/repo", "refs/heads/main")

	err := o.DeliverOne(context.Background(), rec)
	if err == nil {
		t.Fatalf("DeliverOne should error on unknown upstream host")
	}
	if !errors.Is(err, upstream.ErrPermanent) {
		t.Errorf("error should wrap upstream.ErrPermanent; got %v", err)
	}
}

// equalSlices is a small helper so we don't pull reflect into the test file.
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
