// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package prompt

import (
	"bytes"
	"strings"
	"testing"
)

func TestStdio_YesNo_Yes(t *testing.T) {
	for _, ans := range []string{"y\n", "yes\n", "Y\n", "YES\n", "Yes \n"} {
		t.Run(strings.TrimSpace(ans), func(t *testing.T) {
			out := &bytes.Buffer{}
			p := FromIO(strings.NewReader(ans), out)
			if !p.YesNo("install?", false) {
				t.Errorf("YesNo(%q) = false, want true", ans)
			}
		})
	}
}

func TestStdio_YesNo_No(t *testing.T) {
	for _, ans := range []string{"n\n", "no\n", "N\n", "NO\n"} {
		t.Run(strings.TrimSpace(ans), func(t *testing.T) {
			out := &bytes.Buffer{}
			p := FromIO(strings.NewReader(ans), out)
			if p.YesNo("install?", true) {
				t.Errorf("YesNo(%q) = true, want false", ans)
			}
		})
	}
}

func TestStdio_YesNo_EmptyUsesDefault(t *testing.T) {
	// Enter with defaultYes=true → true
	out := &bytes.Buffer{}
	if !FromIO(strings.NewReader("\n"), out).YesNo("?", true) {
		t.Error("empty + defaultYes=true should return true")
	}
	// Enter with defaultYes=false → false
	out = &bytes.Buffer{}
	if FromIO(strings.NewReader("\n"), out).YesNo("?", false) {
		t.Error("empty + defaultYes=false should return false")
	}
}

func TestStdio_YesNo_LoopOnInvalid(t *testing.T) {
	// First two answers are garbage; third is valid yes.
	out := &bytes.Buffer{}
	p := FromIO(strings.NewReader("maybe\nidk\ny\n"), out)
	if !p.YesNo("install?", false) {
		t.Error("expected true on third (valid) answer")
	}
	// The loop should have prompted three times.
	prompts := strings.Count(out.String(), "install?")
	if prompts != 3 {
		t.Errorf("prompted %d times, want 3", prompts)
	}
	// And printed the validation message twice.
	if c := strings.Count(out.String(), "please answer"); c != 2 {
		t.Errorf("validation message shown %d times, want 2", c)
	}
}

func TestStdio_YesNo_EOFFallsBackToDefault(t *testing.T) {
	out := &bytes.Buffer{}
	// Empty input → scanner returns false on Scan() → returns default.
	if !FromIO(strings.NewReader(""), out).YesNo("?", true) {
		t.Error("EOF should fall back to defaultYes=true")
	}
}

func TestStdio_YesNo_PromptHint(t *testing.T) {
	out := &bytes.Buffer{}
	FromIO(strings.NewReader("y\n"), out).YesNo("install?", true)
	if !strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("expected [Y/n] hint with defaultYes=true: %q", out.String())
	}
	out = &bytes.Buffer{}
	FromIO(strings.NewReader("y\n"), out).YesNo("install?", false)
	if !strings.Contains(out.String(), "[y/N]") {
		t.Errorf("expected [y/N] hint with defaultYes=false: %q", out.String())
	}
}

func TestAlways(t *testing.T) {
	if !Always(true).YesNo("anything", false) {
		t.Error("Always(true) should return true regardless of defaultYes")
	}
	if Always(false).YesNo("anything", true) {
		t.Error("Always(false) should return false regardless of defaultYes")
	}
}
