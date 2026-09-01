// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"sort"
	"strings"
	"testing"

	"nimblegate/internal/selection"
	"nimblegate/internal/stdlib"
)

// Every stdlib frame that ships a corpus must grade "passing" against it. The
// runner already existed and nothing ran it across the catalog: `gateway
// doctor` grades one frame as a sanity check and the rest were never exercised,
// so a frame could stop catching what its own positives describe without any
// test noticing.
//
// This is also the answer to grepping `check` output by hand, which is how a
// documentation string quoting a frame's name got miscounted as a finding: the
// assertions come from the runner's per-case results, not from reading text.
func TestStdlibFramesGradeAgainstTheirCorpus(t *testing.T) {
	all, err := stdlib.Load()
	if err != nil {
		t.Fatalf("load stdlib: %v", err)
	}
	checkFns := BuiltinCheckFuncs()

	var graded, pending, unbound []string
	for _, f := range all {
		testdataFS, ok := stdlib.TestdataFS(f.ID())
		if !ok {
			pending = append(pending, f.ID())
			continue
		}
		runFS, ok := testdataFS.(selection.FS)
		if !ok {
			t.Errorf("%s: testdata fs does not implement selection.FS", f.ID())
			continue
		}
		fn, ok := checkFns[f.ID()]
		if !ok {
			unbound = append(unbound, f.ID())
			continue
		}
		res, err := selection.Run(f.ID(), fn, runFS)
		if err != nil {
			t.Errorf("%s: run: %v", f.ID(), err)
			continue
		}
		graded = append(graded, f.ID())
		if res.Grade == "passing" {
			continue
		}
		var failed []string
		for _, c := range res.Cases {
			if !c.Passed {
				failed = append(failed, c.Kind+"/"+c.Filename+" -> "+string(c.Outcome))
			}
		}
		t.Errorf("%s: grade=%s positives %d/%d negatives %d/%d\n    %s",
			f.ID(), res.Grade,
			res.PositivesPassed, res.PositivesTotal,
			res.NegativesPassed, res.NegativesTotal,
			strings.Join(failed, "\n    "))
	}

	sort.Strings(pending)
	sort.Strings(unbound)
	t.Logf("graded %d frame(s) against a corpus", len(graded))
	if len(unbound) > 0 {
		t.Errorf("%d frame(s) ship a corpus with no CheckFunc bound: %s", len(unbound), strings.Join(unbound, ", "))
	}
	// Every frame in the catalog now ships a corpus, so an addition without one
	// is a regression rather than a known gap. Before this was enforced the
	// list stood at 13, including four security frames that turned out to BLOCK
	// legitimate emoji - untested and wrong at the same time.
	if len(pending) > 0 {
		t.Errorf("%d frame(s) ship no corpus, so nothing checks they still catch what they claim:\n    %s",
			len(pending), strings.Join(pending, "\n    "))
	}
}
