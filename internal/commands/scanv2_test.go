// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"reflect"
	"sort"
	"testing"
)

func TestDetectAxes_svelteHighConfidence(t *testing.T) {
	s := scanSignals{SvelteConfig: true, SvelteCount: 3, HTMLCount: 1}
	got := DetectAxes(s)
	if got.Framework != "svelte" || got.FrameworkConfidence != ConfidenceHigh {
		t.Errorf("svelte detection: got %s/%s, want svelte/high", got.Framework, got.FrameworkConfidence)
	}
}

func TestDetectAxes_svelteCountAloneStillHigh(t *testing.T) {
	// Per spec §6.1: svelte fires on EITHER config OR *.svelte files.
	s := scanSignals{SvelteCount: 5}
	got := DetectAxes(s)
	if got.Framework != "svelte" || got.FrameworkConfidence != ConfidenceHigh {
		t.Errorf("svelte (count-only) detection: got %s/%s, want svelte/high", got.Framework, got.FrameworkConfidence)
	}
}

func TestDetectAxes_astroHighConfidence(t *testing.T) {
	s := scanSignals{AstroConfig: true}
	got := DetectAxes(s)
	if got.Framework != "astro" || got.FrameworkConfidence != ConfidenceHigh {
		t.Errorf("astro detection: got %s/%s, want astro/high", got.Framework, got.FrameworkConfidence)
	}
}

func TestDetectAxes_goHighConfidence(t *testing.T) {
	s := scanSignals{GoMod: true}
	got := DetectAxes(s)
	if got.Framework != "go" || got.FrameworkConfidence != ConfidenceHigh {
		t.Errorf("go detection: got %s/%s, want go/high", got.Framework, got.FrameworkConfidence)
	}
}

func TestDetectAxes_htmlIsFallback(t *testing.T) {
	// HTML present + nothing else → framework=html at FALLBACK band.
	s := scanSignals{HTMLCount: 10}
	got := DetectAxes(s)
	if got.Framework != "html" || got.FrameworkConfidence != ConfidenceFallback {
		t.Errorf("html fallback: got %s/%s, want html/fallback", got.Framework, got.FrameworkConfidence)
	}
}

func TestDetectAxes_noSignalsReturnsNone(t *testing.T) {
	got := DetectAxes(scanSignals{})
	if got.Framework != "" || got.FrameworkConfidence != ConfidenceNone {
		t.Errorf("empty scan: got framework %s/%s, want /none", got.Framework, got.FrameworkConfidence)
	}
	if got.Platform != "" || got.PlatformConfidence != ConfidenceNone {
		t.Errorf("empty scan: got platform %s/%s, want /none", got.Platform, got.PlatformConfidence)
	}
}

func TestDetectAxes_cloudflareHighConfidence(t *testing.T) {
	s := scanSignals{WranglerToml: true, HTMLCount: 5}
	got := DetectAxes(s)
	if got.Platform != "cloudflare" || got.PlatformConfidence != ConfidenceHigh {
		t.Errorf("cloudflare detection: got %s/%s, want cloudflare/high", got.Platform, got.PlatformConfidence)
	}
}

func TestDetectAxes_staticHostFallback(t *testing.T) {
	s := scanSignals{HTMLCount: 12}
	got := DetectAxes(s)
	if got.Platform != "static-host" || got.PlatformConfidence != ConfidenceFallback {
		t.Errorf("static-host fallback: got %s/%s, want static-host/fallback", got.Platform, got.PlatformConfidence)
	}
}

func TestDetectAxes_svelteConflictsWithAstro(t *testing.T) {
	// Both Svelte AND Astro signals → conflict on framework axis.
	s := scanSignals{SvelteConfig: true, AstroConfig: true}
	got := DetectAxes(s)
	if got.Framework != "" {
		t.Errorf("expected empty Framework on conflict; got %q", got.Framework)
	}
	hasConflict := false
	for _, c := range got.Conflicts {
		if c == "framework" {
			hasConflict = true
			break
		}
	}
	if !hasConflict {
		t.Errorf("expected 'framework' in Conflicts; got %v", got.Conflicts)
	}
	// Candidates should list both for the prompt.
	cands := got.CandidatesByAxis["framework"]
	sort.Strings(cands)
	if !reflect.DeepEqual(cands, []string{"astro", "svelte"}) {
		t.Errorf("expected [astro svelte] candidates, got %v", cands)
	}
}

func TestDetectAxes_myappShapeProducesHtmlPlusCloudflare(t *testing.T) {
	// myapp ships HTML + wrangler.toml (CF Pages). DetectAxes should
	// pick html/fallback + cloudflare/high - the operator can then strip
	// cf-workers via [platform.cloudflare].exclude as v2 expects.
	s := scanSignals{WranglerToml: true, HTMLCount: 28}
	got := DetectAxes(s)
	if got.Framework != "html" {
		t.Errorf("myapp framework: got %q, want html", got.Framework)
	}
	if got.Platform != "cloudflare" {
		t.Errorf("myapp platform: got %q, want cloudflare", got.Platform)
	}
	if len(got.Conflicts) != 0 {
		t.Errorf("myapp shape should have no conflicts; got %v", got.Conflicts)
	}
}

func TestConfidence_StringStable(t *testing.T) {
	cases := []struct {
		c    Confidence
		want string
	}{
		{ConfidenceNone, "none"},
		{ConfidenceFallback, "fallback"},
		{ConfidenceMedium, "medium"},
		{ConfidenceHigh, "high"},
	}
	for _, c := range cases {
		if got := c.c.String(); got != c.want {
			t.Errorf("Confidence(%d).String() = %q, want %q", c.c, got, c.want)
		}
	}
}
