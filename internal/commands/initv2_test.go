// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestResolveAxes_flagOverridesDetection(t *testing.T) {
	// Manual override always wins per spec §6.4.
	detected := AxisRecommendation{
		Framework:           "svelte",
		FrameworkConfidence: ConfidenceHigh,
		Platform:            "cloudflare",
		PlatformConfidence:  ConfidenceHigh,
	}
	d := ResolveAxes(detected, "astro", "vercel")
	if d.Framework != "astro" {
		t.Errorf("framework flag override: got %q, want astro", d.Framework)
	}
	if d.Platform != "vercel" {
		t.Errorf("platform flag override: got %q, want vercel", d.Platform)
	}
	if d.PromptFramework || d.PromptPlatform {
		t.Errorf("flag-supplied axes should never prompt; got fw=%v pf=%v", d.PromptFramework, d.PromptPlatform)
	}
}

func TestResolveAxes_cleanDetectionPassesThrough(t *testing.T) {
	detected := AxisRecommendation{
		Framework:           "svelte",
		FrameworkConfidence: ConfidenceHigh,
		Platform:            "cloudflare",
		PlatformConfidence:  ConfidenceHigh,
	}
	d := ResolveAxes(detected, "", "")
	if d.Framework != "svelte" || d.Platform != "cloudflare" {
		t.Errorf("clean detection: got %q/%q, want svelte/cloudflare", d.Framework, d.Platform)
	}
	if d.PromptFramework || d.PromptPlatform {
		t.Errorf("clean detection should not prompt")
	}
}

func TestResolveAxes_conflictRaisesPromptFlag(t *testing.T) {
	detected := AxisRecommendation{
		Conflicts:        []string{"framework"},
		CandidatesByAxis: map[string][]string{"framework": {"svelte", "astro"}},
	}
	d := ResolveAxes(detected, "", "")
	if !d.PromptFramework {
		t.Error("framework conflict should set PromptFramework=true")
	}
	if d.Framework != "" {
		t.Errorf("conflicted framework should be empty; got %q", d.Framework)
	}
}

func TestResolveAxes_flagDefusesConflict(t *testing.T) {
	// If conflict is present BUT flag is supplied, no prompt.
	detected := AxisRecommendation{
		Conflicts:        []string{"framework"},
		CandidatesByAxis: map[string][]string{"framework": {"svelte", "astro"}},
	}
	d := ResolveAxes(detected, "svelte", "")
	if d.PromptFramework {
		t.Error("flag should defuse conflict; PromptFramework should be false")
	}
	if d.Framework != "svelte" {
		t.Errorf("flag value should win; got %q", d.Framework)
	}
}

func TestResolveAxes_noSignalLeavesAxisEmpty(t *testing.T) {
	// No detection, no flag, no conflict → axis stays empty + no prompt.
	// (Framework is optional in v2; not every project has one.)
	d := ResolveAxes(AxisRecommendation{}, "", "")
	if d.Framework != "" || d.Platform != "" {
		t.Errorf("no-signal: expected empty axes; got %q/%q", d.Framework, d.Platform)
	}
	if d.PromptFramework || d.PromptPlatform {
		t.Errorf("no-signal should not prompt; got fw=%v pf=%v", d.PromptFramework, d.PromptPlatform)
	}
}

func TestResolveAxes_bothAxesConflictBothPrompt(t *testing.T) {
	// Future case once vercel/netlify signals exist; structurally the
	// resolver must already handle both axes conflicting at once.
	detected := AxisRecommendation{
		Conflicts: []string{"framework", "platform"},
		CandidatesByAxis: map[string][]string{
			"framework": {"svelte", "astro"},
			"platform":  {"cloudflare", "vercel"},
		},
	}
	d := ResolveAxes(detected, "", "")
	if !d.PromptFramework || !d.PromptPlatform {
		t.Errorf("both-conflict: expected both prompts; got fw=%v pf=%v", d.PromptFramework, d.PromptPlatform)
	}
}

func TestPromptAxis_validChoiceReturnsValue(t *testing.T) {
	in := strings.NewReader("2\n")
	var out bytes.Buffer
	got, err := PromptAxis(in, &out, "framework", []string{"svelte", "astro"})
	if err != nil {
		t.Fatalf("PromptAxis: %v", err)
	}
	// Sorted alphabetically: 1=astro, 2=svelte
	if got != "svelte" {
		t.Errorf("got %q, want svelte (choice 2 in sorted list)", got)
	}
	if !strings.Contains(out.String(), "Multiple framework signals detected") {
		t.Errorf("expected prompt header in output; got: %s", out.String())
	}
}

func TestPromptAxis_retriesOnInvalidInput(t *testing.T) {
	// First two attempts invalid, third valid.
	in := strings.NewReader("garbage\n99\n1\n")
	var out bytes.Buffer
	got, err := PromptAxis(in, &out, "platform", []string{"cloudflare", "vercel"})
	if err != nil {
		t.Fatalf("PromptAxis: %v", err)
	}
	if got != "cloudflare" {
		t.Errorf("got %q, want cloudflare (choice 1)", got)
	}
	if strings.Count(out.String(), "invalid choice") != 2 {
		t.Errorf("expected 2 invalid-choice messages; got: %s", out.String())
	}
}

func TestPromptAxis_giveUpAfter3Attempts(t *testing.T) {
	in := strings.NewReader("a\nb\nc\n")
	var out bytes.Buffer
	_, err := PromptAxis(in, &out, "framework", []string{"svelte", "astro"})
	if err == nil {
		t.Error("expected error after 3 invalid attempts")
	}
}

func TestPromptAxis_eofReturnsError(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer
	_, err := PromptAxis(in, &out, "framework", []string{"svelte"})
	if err == nil {
		t.Error("expected error on EOF")
	}
}

func TestNonInteractiveAmbiguityError_listsAllConflictedAxes(t *testing.T) {
	detected := AxisRecommendation{
		CandidatesByAxis: map[string][]string{
			"framework": {"svelte", "astro"},
			"platform":  {"cloudflare", "vercel"},
		},
	}
	d := AxisDecision{PromptFramework: true, PromptPlatform: true}
	err := NonInteractiveAmbiguityError(d, detected)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"not a TTY",
		"--framework=",
		"--platform=",
		"svelte",
		"astro",
		"cloudflare",
		"vercel",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q; got: %s", want, msg)
		}
	}
}

func TestNonInteractiveAmbiguityError_onlyConflictedAxesListed(t *testing.T) {
	// Only framework conflicts - platform suggestion should NOT appear.
	detected := AxisRecommendation{
		CandidatesByAxis: map[string][]string{
			"framework": {"svelte", "astro"},
		},
	}
	d := AxisDecision{PromptFramework: true, PromptPlatform: false}
	err := NonInteractiveAmbiguityError(d, detected)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--framework=") {
		t.Errorf("expected --framework suggestion; got: %s", msg)
	}
	if strings.Contains(msg, "--platform=") {
		t.Errorf("unconflicted platform should NOT appear; got: %s", msg)
	}
}
