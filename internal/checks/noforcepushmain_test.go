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

func TestNoForcePushMain_SpellingsGitAccepts(t *testing.T) {
	block := []string{
		"git -C /r push --force origin main",
		"git push -fu origin main",
		"git push --forc origin main",
		"git push --force-with-lease=main origin main",
		"git push origin +main",
		"git push origin +HEAD:main",
		"git push --force origin HEAD:refs/heads/main",
	}
	for _, cmd := range block {
		if got := NoForcePushMain(engine.CheckContext{Trigger: engine.TriggerGitWrap, Command: cmd}); got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", cmd, got.Outcome)
		}
	}
	pass := []string{
		"git push origin main",
		"git push --follow-tags origin main",
		"git push -u origin main",
		"git push -o ci.skip origin main",
		"git push origin +feature",
	}
	for _, cmd := range pass {
		if got := NoForcePushMain(engine.CheckContext{Trigger: engine.TriggerGitWrap, Command: cmd}); got.Outcome == engine.OutcomeBlock {
			t.Errorf("%q: outcome = BLOCK, want not blocked", cmd)
		}
	}
}
