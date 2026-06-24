// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func TestAptPurgePreview_PurgeWithSimulateFlagPasses(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "apt purge --simulate rpcbind",
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (simulate IS the preview)", got.Outcome)
	}
}

func TestAptPurgePreview_PurgeWithoutSimulateBlocks(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "apt purge rpcbind",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestAptPurgePreview_RemoveWithoutSimulateBlocks(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "apt-get remove rpcbind",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestAptPurgePreview_OtherAptCommandsSkip(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "apt install rpcbind",
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP", got.Outcome)
	}
}
