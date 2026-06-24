// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func TestNoForcePushMain_ForcePushMainBlocks(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "git push --force origin main",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoForcePushMain_ShortForceFlagBlocks(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "git push -f origin master",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoForcePushMain_RegularPushPasses(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "git push origin main",
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestNoForcePushMain_ForcePushFeatureBranchPasses(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "git push --force origin feature/abc",
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (feature branch not protected)", got.Outcome)
	}
}

func TestNoForcePushMain_NonPushCommandSkips(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "git commit -m 'msg'",
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP", got.Outcome)
	}
}
