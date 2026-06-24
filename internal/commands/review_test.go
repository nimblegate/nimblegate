// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/tasks"
)

func TestReviewBuckets(t *testing.T) {
	ledger := tasks.NewLedger()
	deferredKey := tasks.FindingKey{FrameID: "security/no-private-keys-in-repo", File: "k.pem", Label: "key"}
	ledger.Tasks[deferredKey.ID()] = &tasks.Task{ID: deferredKey.ID(), Status: tasks.StatusDeferred, Severity: "BLOCK"}

	findings := []tasks.Finding{
		{Key: tasks.FindingKey{FrameID: "security/no-hardcoded-credentials", File: "a", Label: "tok"}, Severity: "BLOCK"}, // dangerous
		{Key: deferredKey, Severity: "BLOCK"}, // deferred (not dangerous-open)
		{Key: tasks.FindingKey{FrameID: "convention/html-seo-meta", File: "b", Label: "meta"}, Severity: "WARN"}, // advisory
		{Key: tasks.FindingKey{FrameID: "convention/x", File: "c", Label: "y"}, Severity: "INFO"},                // advisory
	}
	dangerous, advisory, deferred := reviewBuckets(findings, ledger)
	if len(dangerous) != 1 || dangerous[0].Key.FrameID != "security/no-hardcoded-credentials" {
		t.Errorf("dangerous = %+v, want 1 (open BLOCK)", dangerous)
	}
	if len(advisory) != 2 {
		t.Errorf("advisory = %d, want 2 (WARN+INFO)", len(advisory))
	}
	if len(deferred) != 1 {
		t.Errorf("deferred = %d, want 1", len(deferred))
	}
}

func TestReviewVerdict_readyWhenNoDangerous(t *testing.T) {
	findings := []tasks.Finding{
		{Key: tasks.FindingKey{FrameID: "convention/x", File: "a", Label: "y"}, Severity: "WARN"},
	}
	dangerous, _, _ := reviewBuckets(findings, tasks.NewLedger())
	if len(dangerous) != 0 {
		t.Errorf("WARN-only project should be production-ready (0 dangerous), got %d", len(dangerous))
	}
}
