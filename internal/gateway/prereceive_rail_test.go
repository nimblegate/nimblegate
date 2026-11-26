// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway/notification"
)

// observe-pr-comments = true makes an observe-mode would-block notify as
// "push.observed", carrying the repo's mention and delivery settings. The
// inline attempt is skipped (its latency would be visible to the observed
// agent), so the record stays queued for the daemon.
func TestRunPreReceive_ObservePRComments_QueuesObservedWithoutInline(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	deps.Policy.Observe = true
	deps.NotificationConfig = &NotificationConfig{
		Enabled:           true,
		ObservePRComments: true,
		UpstreamKind:      "stub",
		Mention:           MentionConfig{IncludePRAssignees: false},
		Delivery:          DeliveryConfig{MaxAttempts: 3, BackoffSchedule: []time.Duration{10 * time.Second}},
	}

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 0 {
		t.Fatalf("observe mode must relay (exit 0), got %d", code)
	}
	records, _ := notification.ReadQueueRecords(filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl"))
	if len(records) != 1 {
		t.Fatalf("observe-pr-comments should queue one record (and not deliver inline), got %d", len(records))
	}
	r := records[0]
	if r.Notification.Event != "push.observed" {
		t.Errorf("Event = %q, want push.observed", r.Notification.Event)
	}
	if !r.OmitPRPeople {
		t.Error("include-pr-assignees = false must ride on the record as OmitPRPeople")
	}
	if r.Delivery.MaxAttempts != 3 || len(r.Delivery.Backoff) != 1 || r.Delivery.Backoff[0] != 10*time.Second {
		t.Errorf("per-repo delivery policy not carried: %+v", r.Delivery)
	}
}

// An inline delivery that succeeds removes the record and records the
// delivery, so the feed can show "delivered" from evidence.
func TestRunPreReceive_InlineDelivery_RecordsOutcome(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t,
		[]engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}},
		nil,
	)
	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub", Mention: MentionConfig{IncludePRAssignees: true}}

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 1 {
		t.Fatalf("expected reject, got %d", code)
	}
	repoDir := filepath.Join(policyRoot, "demo")
	if records, _ := notification.ReadQueueRecords(filepath.Join(repoDir, "pr-comment-queue.jsonl")); len(records) != 0 {
		t.Fatalf("inline delivery should drain the record, %d left", len(records))
	}
	outcomes := notification.ReadOutcomes(filepath.Join(repoDir, notification.OutcomesFile))
	if len(outcomes) != 1 {
		t.Fatalf("want one recorded outcome, got %+v", outcomes)
	}
	for _, o := range outcomes {
		if o.Outcome != notification.OutcomeDelivered {
			t.Errorf("outcome = %q, want delivered", o.Outcome)
		}
	}
}

// include-pr-assignees defaults to true even when [notification.mention] is
// present without the key; only an explicit false turns tagging off.
func TestFilePolicyStore_Load_IncludePRAssigneesDefault(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{{"", true}, {"include-pr-assignees = false", false}, {"include-pr-assignees = true", true}} {
		root := t.TempDir()
		writeRepoTOML(t, root, "demo", `
upstream-url   = "https://example.com/demo.git"
protected-refs = ["refs/heads/main"]
enabled        = true

[notification]
enabled = true

[notification.mention]
default = "@bot"
`+tc.line+"\n")
		p, err := FilePolicyStore{Root: root}.Load("demo")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := p.Notification.Mention.IncludePRAssignees; got != tc.want {
			t.Errorf("%q: IncludePRAssignees = %v, want %v", tc.line, got, tc.want)
		}
	}
}
