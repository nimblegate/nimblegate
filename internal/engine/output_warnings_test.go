// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestFormatLoadWarnings_EmptyNoOutput(t *testing.T) {
	var buf bytes.Buffer
	n := FormatLoadWarnings(&buf, nil)
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty errs; got: %q", buf.String())
	}
}

func TestFormatLoadWarnings_PrintsCountAndDetails(t *testing.T) {
	errs := []error{
		errors.New("/proj/.appframes/a.md: frontmatter: 'category' is required"),
		errors.New("/proj/.appframes/b.md: unclosed frontmatter"),
	}
	var buf bytes.Buffer
	n := FormatLoadWarnings(&buf, errs)
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	out := buf.String()
	if !strings.Contains(out, "2 frame(s) failed to load") {
		t.Errorf("missing count header; got: %s", out)
	}
	if !strings.Contains(out, "nimblegate lint") {
		t.Errorf("missing remediation hint; got: %s", out)
	}
	if !strings.Contains(out, "a.md") || !strings.Contains(out, "b.md") {
		t.Errorf("missing individual error lines; got: %s", out)
	}
}
