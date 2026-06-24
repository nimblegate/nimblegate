// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestCFGraphQLSchema_BlocksCountOn1dGroups(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "queries.graphql", `query {
  httpRequests1dGroups(filter: { date_geq: "2026-05-11" date_leq: "2026-05-18" }) {
    count
  }
}
`)
	got := CFGraphQLSchemaMatch(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (count on grouped dataset)\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "count") || !strings.Contains(got.Reason, "1dGroups") {
		t.Errorf("reason should mention count + 1dGroups; got: %s", got.Reason)
	}
}

func TestCFGraphQLSchema_BlocksSumOnAdaptive(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "queries.graphql", `query {
  httpRequestsAdaptiveGroups(filter: { datetime_geq: "2026-05-18T00:00:00Z" datetime_leq: "2026-05-18T20:00:00Z" }) {
    sum { requests }
  }
}
`)
	got := CFGraphQLSchemaMatch(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (sum on adaptive)\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "sum") || !strings.Contains(got.Reason, "Adaptive") {
		t.Errorf("reason should mention sum + Adaptive; got: %s", got.Reason)
	}
}

func TestCFGraphQLSchema_PassesValidCombos(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "queries.graphql", `query {
  httpRequestsAdaptiveGroups(filter: { datetime_geq: "2026-05-18T00:00:00Z" }) {
    count
  }
  httpRequests1dGroups(filter: { date_geq: "2026-05-11" }) {
    sum { requests }
  }
}
`)
	got := CFGraphQLSchemaMatch(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (both combos are valid)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFGraphQLSchema_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "queries.graphql", `query {
  # appframes:disable-next-line app-correctness/cf-graphql-schema-match
  httpRequests1dGroups(filter: { date_geq: "2026-05-11" }) {
    count
  }
}
`)
	got := CFGraphQLSchemaMatch(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (line disable)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFGraphQLSchema_NonApplicableFileIgnored(t *testing.T) {
	root := t.TempDir()
	// README mentioning the wrong combo - not applicable.
	writeGQL(t, root, "README.md", `Use httpRequests1dGroups with count.`)
	got := CFGraphQLSchemaMatch(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (markdown is not applicable)", got.Outcome)
	}
}
