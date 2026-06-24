// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"regexp"
	"testing"
)

func TestParseESLintJSON(t *testing.T) {
	out := []byte(`[
	  {"filePath":"/proj/src/a.js","messages":[
	    {"ruleId":"no-unused-vars","severity":2,"message":"'x' is defined but never used.","line":3,"column":5},
	    {"ruleId":null,"severity":2,"message":"Parsing error: bad","line":1,"column":1}
	  ]},
	  {"filePath":"/proj/src/clean.js","messages":[]}
	]`)
	hits, ok := parseESLintJSON(out, "/proj")
	if !ok {
		t.Fatal("valid eslint JSON should parse ok")
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].File != "src/a.js" || hits[0].Line != 3 || hits[0].Label != "no-unused-vars: 'x' is defined but never used." {
		t.Errorf("hit0 = %+v", hits[0])
	}
	if hits[1].Label != "Parsing error: bad" { // null ruleId → message only
		t.Errorf("hit1 label = %q", hits[1].Label)
	}
}

func TestParseESLintJSON_invalidIsNotOK(t *testing.T) {
	if _, ok := parseESLintJSON([]byte("Cannot find config\n"), "/proj"); ok {
		t.Error("non-JSON eslint output should report ok=false (tool error, not clean)")
	}
}

func TestParseShellcheckJSON(t *testing.T) {
	out := []byte(`[
	  {"file":"deploy.sh","line":12,"column":3,"level":"warning","code":2086,"message":"Double quote to prevent globbing."},
	  {"file":"deploy.sh","line":20,"column":1,"level":"error","code":1009,"message":"Parsing error."}
	]`)
	hits, ok := parseShellcheckJSON(out, "/proj")
	if !ok || len(hits) != 2 {
		t.Fatalf("got ok=%v hits=%d: %+v", ok, len(hits), hits)
	}
	if hits[0].File != "deploy.sh" || hits[0].Line != 12 || hits[0].Label != "SC2086 (warning): Double quote to prevent globbing." {
		t.Errorf("hit0 = %+v", hits[0])
	}
}

func TestParseRegexOutput(t *testing.T) {
	// a generic `file:line:col: message` linter
	re := regexp.MustCompile(`^(?P<file>[^:]+):(?P<line>\d+):\d+:\s*(?P<msg>.+)$`)
	out := "src/x.py:10:5: E501 line too long\nsome noise line\nsrc/y.py:3:1: F401 unused import\n"
	hits := parseRegexOutput(out, "/proj", re)
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].File != "src/x.py" || hits[0].Line != 10 || hits[0].Label != "E501 line too long" {
		t.Errorf("hit0 = %+v", hits[0])
	}
}
