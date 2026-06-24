// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"strings"
	"testing"

	"nimblegate/internal/tasks"
)

func TestFixPrompt(t *testing.T) {
	task := &tasks.Task{
		FrameID:  "security/no-innerHTML-user-input",
		File:     "tools/qr/app.js",
		Line:     209,
		Label:    "innerHTML non-literal assignment",
		Severity: "WARN",
	}
	p := fixPrompt(task)
	for _, want := range []string{
		"security/no-innerHTML-user-input",
		"tools/qr/app.js:209",
		"innerHTML non-literal assignment",
		"minimal",
		"Do NOT disable", // must not suppress
	} {
		if !strings.Contains(p, want) {
			t.Errorf("fix prompt missing %q:\n%s", want, p)
		}
	}
}

func TestSelectFixTargets(t *testing.T) {
	l := tasks.NewLedger()
	l.Tasks["a"] = &tasks.Task{ID: "a", FrameID: "security/x", Severity: "BLOCK", Status: tasks.StatusOpen}
	l.Tasks["b"] = &tasks.Task{ID: "b", FrameID: "convention/y", Severity: "WARN", Status: tasks.StatusOpen}

	if got, _ := selectFixTargets(l, "", true, false); len(got) != 2 { // --all
		t.Errorf("--all = %d, want 2", len(got))
	}
	if got, _ := selectFixTargets(l, "", false, true); len(got) != 1 || got[0].Severity != "BLOCK" { // --dangerous
		t.Errorf("--dangerous = %+v, want 1 BLOCK", got)
	}
	if got, _ := selectFixTargets(l, "a", false, false); len(got) != 1 || got[0].ID != "a" { // by id
		t.Errorf("by-id = %+v, want task a", got)
	}
}
