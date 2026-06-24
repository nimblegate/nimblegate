// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"testing"

	"nimblegate/internal/config"
)

func TestDescribeEnabledAndByID(t *testing.T) {
	lc := map[string]config.LinterConfig{
		"eslint":     {Enabled: true, Severity: "warn", Dir: "studio", Patterns: []string{"src"}},
		"go-vet":     {Enabled: true}, // no severity → BLOCK default
		"mycustom":   {Enabled: true, Command: "mylint", Args: []string{"--strict"}},
		"shellcheck": {Enabled: false},
	}

	infos := DescribeEnabled(lc)
	if len(infos) != 3 {
		t.Fatalf("DescribeEnabled = %d, want 3 (shellcheck disabled): %+v", len(infos), infos)
	}
	for i, want := range []string{"eslint", "go-vet", "mycustom"} { // sorted
		if infos[i].Name != want {
			t.Fatalf("infos[%d].Name = %s, want %s", i, infos[i].Name, want)
		}
	}
	if es := infos[0]; es.ID != "app-correctness/eslint" || !es.Builtin || es.Severity != "WARN" {
		t.Errorf("eslint info = %+v, want id=app-correctness/eslint builtin sev=WARN", es)
	}
	if gv := infos[1]; gv.Severity != "BLOCK" {
		t.Errorf("go-vet default severity = %s, want BLOCK", gv.Severity)
	}
	if cu := infos[2]; cu.Builtin || cu.Command != "mylint" {
		t.Errorf("custom info = %+v, want builtin=false command=mylint", cu)
	}

	if _, ok := ByID("app-correctness/eslint", lc); !ok {
		t.Error("ByID(eslint) should resolve an enabled linter")
	}
	if _, ok := ByID("app-correctness/shellcheck", lc); ok {
		t.Error("ByID(shellcheck) must NOT resolve - it's disabled")
	}
	if _, ok := ByID("app-correctness/cf-graphql-dataset-by-window", lc); ok {
		t.Error("ByID must NOT treat a real app-correctness/* frame id as a linter")
	}
}
