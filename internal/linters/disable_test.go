// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

func TestRuleDisabled_boundaryMatch(t *testing.T) {
	cases := []struct {
		label   string
		disable []string
		want    bool
	}{
		{"SC1091 (info): Not following", []string{"SC1091"}, true},
		{"SC1091 (info): x", []string{"SC10"}, false}, // word boundary: SC10 ≠ SC1091
		{"no-unused-vars: 'x' unused", []string{"no-unused-vars"}, true},
		{"printf: bad format", []string{"composites", "printf"}, true},
		{"SC2086 (info): quote", []string{"SC1091"}, false}, // different code
		{"printf: x", nil, false},
	}
	for _, c := range cases {
		if got := ruleDisabled(c.label, c.disable); got != c.want {
			t.Errorf("ruleDisabled(%q, %v) = %v, want %v", c.label, c.disable, got, c.want)
		}
	}
}

func TestEnabledIDs(t *testing.T) {
	lc := map[string]config.LinterConfig{
		"go-vet":     {Enabled: true},
		"shellcheck": {Enabled: true},
		"eslint":     {Enabled: false}, // disabled → excluded
	}
	got := EnabledIDs(lc)
	want := []string{"app-correctness/go-vet", "app-correctness/shellcheck"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("EnabledIDs = %v, want %v (sorted, enabled only)", got, want)
	}
}

func TestDropDisabled(t *testing.T) {
	hits := []engine.Hit{
		{Label: "SC1091 (info): a"},
		{Label: "SC2086 (info): b"},
		{Label: "SC1091 (info): c"},
	}
	kept := dropDisabled(hits, []string{"SC1091"})
	if len(kept) != 1 || kept[0].Label != "SC2086 (info): b" {
		t.Fatalf("dropDisabled kept %+v, want only SC2086", kept)
	}
}

func TestBuildResult_allDisabledIsPass(t *testing.T) {
	hits := []engine.Hit{{Label: "SC1091 (info): a"}}
	res := buildResult("app-correctness/shellcheck", "shellcheck", hits, engine.OutcomeWarn, []string{"SC1091"})
	if res.Outcome != engine.OutcomePass {
		t.Errorf("all-disabled → PASS, got %s", res.Outcome)
	}
}
