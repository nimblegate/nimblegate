// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/tasks"
)

func TestFilterBlocking_openBlockBlocksDeferredAndWarnDoNot(t *testing.T) {
	ledger := tasks.NewLedger()
	deferred := tasks.FindingKey{FrameID: "fs-safety/rm-rf-protected-paths", File: "wipe.sh", Label: "rm -rf"}
	ledger.Tasks[deferred.ID()] = &tasks.Task{ID: deferred.ID(), Status: tasks.StatusDeferred, Severity: "BLOCK"}

	findings := []tasks.Finding{
		{Key: tasks.FindingKey{FrameID: "security/no-hardcoded-credentials", File: "cfg.js", Label: "key"}, Severity: "BLOCK"}, // open BLOCK → blocks
		{Key: deferred, Severity: "BLOCK"}, // deferred BLOCK → allowed
		{Key: tasks.FindingKey{FrameID: "security/no-innerHTML-user-input", File: "a.js", Label: "innerHTML"}, Severity: "WARN"}, // WARN → never blocks push
	}
	got := filterBlocking(findings, ledger)
	if len(got) != 1 {
		t.Fatalf("blocking = %d, want 1 (only the open BLOCK): %+v", len(got), got)
	}
	if got[0].Key.FrameID != "security/no-hardcoded-credentials" {
		t.Errorf("wrong blocking finding: %+v", got[0])
	}
}

func TestFilterBlocking_noBlocksIsEmpty(t *testing.T) {
	findings := []tasks.Finding{
		{Key: tasks.FindingKey{FrameID: "convention/x", File: "a", Label: "l"}, Severity: "WARN"},
		{Key: tasks.FindingKey{FrameID: "convention/y", File: "b", Label: "m"}, Severity: "INFO"},
	}
	if got := filterBlocking(findings, tasks.NewLedger()); len(got) != 0 {
		t.Errorf("no BLOCK findings → nothing blocks, got %d", len(got))
	}
}
