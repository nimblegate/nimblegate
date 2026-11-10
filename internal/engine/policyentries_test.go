// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"reflect"
	"strings"
	"testing"
)

var policyKnown = map[string]bool{
	"security/no-hardcoded-credentials": true,
	"security/no-env-file-in-repo":      true,
	"git/no-force-push-main":            true,
}

func TestUnknownEnabledEntries(t *testing.T) {
	cases := []struct {
		name    string
		enabled []string
		unknown []string
		none    bool
	}{
		{"empty list means all frames", nil, nil, false},
		{"all known", []string{"security/no-hardcoded-credentials", "git/no-force-push-main"}, nil, false},
		{"typo beside a valid frame", []string{"security/no-hardcoded-credentials", "security/no-hardcoded-credential"}, []string{"security/no-hardcoded-credential"}, false},
		{"group only", []string{"@tier-1", "@web"}, []string{"@tier-1", "@web"}, true},
		{"group named like a frame is still unknown", []string{"@security/no-env-file-in-repo"}, []string{"@security/no-env-file-in-repo"}, true},
		{"category pattern matching a frame", []string{"security/*"}, nil, false},
		{"category pattern matching nothing", []string{"nosuch/*", "git/no-force-push-main"}, []string{"nosuch/*"}, false},
		{"bare wildcard", []string{"/*"}, []string{"/*"}, true},
		{"prefix without slash is not a category", []string{"sec*"}, []string{"sec*"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unknown, none := UnknownEnabledEntries(c.enabled, policyKnown)
			if !reflect.DeepEqual(unknown, c.unknown) || none != c.none {
				t.Errorf("got (%q, %v), want (%q, %v)", unknown, none, c.unknown, c.none)
			}
		})
	}
}

func TestPolicyEntriesResults(t *testing.T) {
	if got := PolicyEntriesResults(nil, policyKnown); got != nil {
		t.Errorf("empty list: want no result, got %+v", got)
	}
	if got := PolicyEntriesResults([]string{"security/*"}, policyKnown); got != nil {
		t.Errorf("resolving list: want no result, got %+v", got)
	}

	warn := PolicyEntriesResults([]string{"git/no-force-push-main", "security/no-hardcoded-credential"}, policyKnown)
	if len(warn) != 1 || warn[0].FrameID != PolicyEntriesID || warn[0].Outcome != OutcomeWarn {
		t.Fatalf("typo: want one WARN %s, got %+v", PolicyEntriesID, warn)
	}
	if !strings.Contains(warn[0].Reason, "security/no-hardcoded-credential") || strings.Contains(warn[0].Reason, "nimblegate lint") {
		t.Errorf("typo reason should name the entry and carry no group hint: %q", warn[0].Reason)
	}

	block := PolicyEntriesResults([]string{"@tier-1"}, policyKnown)
	if len(block) != 1 || block[0].Outcome != OutcomeBlock {
		t.Fatalf("group only: want one BLOCK, got %+v", block)
	}
	for _, want := range []string{"policy enables no checks", "@tier-1", "nimblegate lint"} {
		if !strings.Contains(block[0].Reason, want) {
			t.Errorf("block reason missing %q: %q", want, block[0].Reason)
		}
	}
}

func TestPolicyEntryLabels_sanitises(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := PolicyEntryLabels([]string{"sec\x1b[31murity/a\n", "b‮c​", long, ""})
	if got[0] != "sec[31murity/a" {
		t.Errorf("control chars not stripped: %q", got[0])
	}
	if got[1] != "bc" {
		t.Errorf("format chars not stripped: %q", got[1])
	}
	if got[2] != strings.Repeat("x", policyEntryRuneCap)+"..." {
		t.Errorf("long entry not capped: %q", got[2])
	}
	if got[3] != `""` {
		t.Errorf("empty entry should render visibly, got %q", got[3])
	}

	many := make([]string, 25)
	for i := range many {
		many[i] = "x/y"
	}
	labels := PolicyEntryLabels(many)
	if len(labels) != policyEntryListCap+1 || labels[policyEntryListCap] != "and 15 more" {
		t.Errorf("list not capped: %q", labels)
	}
}
