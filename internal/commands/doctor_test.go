// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"
)

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		name   string
		checks []doctorCheck
		want   string
	}{
		{
			"all OK",
			[]doctorCheck{
				{Name: "a", Status: "OK"},
				{Name: "b", Status: "OK"},
			},
			"OK",
		},
		{
			"SKIPs don't degrade",
			[]doctorCheck{
				{Name: "a", Status: "OK"},
				{Name: "b", Status: "SKIP"},
			},
			"OK",
		},
		{
			"FAIL wins over SKIP + OK",
			[]doctorCheck{
				{Name: "a", Status: "OK"},
				{Name: "b", Status: "SKIP"},
				{Name: "c", Status: "FAIL"},
			},
			"FAIL",
		},
		{
			"all FAIL",
			[]doctorCheck{
				{Name: "a", Status: "FAIL"},
			},
			"FAIL",
		},
		{
			"empty list",
			[]doctorCheck{},
			"OK",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := aggregateStatus(c.checks); got != c.want {
				t.Errorf("aggregateStatus: got %q, want %q", got, c.want)
			}
		})
	}
}

func TestExitCodeForStatus(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"OK", 0},
		{"FAIL", 2},
		{"weird", 2},
	}
	for _, c := range cases {
		if got := exitCodeForStatus(c.in); got != c.want {
			t.Errorf("exitCodeForStatus(%q): got %d, want %d", c.in, got, c.want)
		}
	}
}

// TestCheckStdlib_OK confirms the embedded stdlib loads cleanly. If
// this fails the binary is corrupted; the test suite would have caught
// it at build time normally.
func TestCheckStdlib_OK(t *testing.T) {
	c := checkStdlib()
	if c.Status != "OK" {
		t.Errorf("stdlib check: got %s (%s); expected OK", c.Status, c.Reason)
	}
}

// TestCheckPatternLink_OK confirms every stdlib frame's pattern field
// references a real pattern. Same invariant as the existing
// commands.TestFramesReferenceExistingPatterns but exercised through
// the doctor codepath.
func TestCheckPatternLink_OK(t *testing.T) {
	c := checkPatternLink()
	if c.Status != "OK" {
		t.Errorf("pattern-link check: got %s (%s); expected OK", c.Status, c.Reason)
	}
}

// TestCheckSelectionSanity_OK runs the negative-selection test end-to-end
// for the doctor's chosen sanity frame. Catches regressions in the
// runner / embed / testdata pipeline.
func TestCheckSelectionSanity_OK(t *testing.T) {
	c := checkSelectionSanity()
	if c.Status != "OK" {
		t.Errorf("selection-sanity check: got %s (%s); expected OK", c.Status, c.Reason)
	}
}
