// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/engine"
)

func writeGQL(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCFGraphQL_BlocksAdaptiveOver7Days(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "infra/n8n/workflows/marketing.graphql", `query {
  viewer {
    accounts {
      httpRequestsAdaptiveGroups(
        limit: 1000
        filter: { datetime_geq: "2026-05-11T00:00:00Z" datetime_leq: "2026-05-18T00:00:00Z" }
      ) {
        count
      }
    }
  }
}
`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "httpRequestsAdaptiveGroups") {
		t.Errorf("expected reason to mention dataset; got: %s", got.Reason)
	}
}

func TestCFGraphQL_PassesAdaptiveUnder1Day(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "infra/n8n/workflows/recent.graphql", `query {
  httpRequestsAdaptiveGroups(
    filter: { datetime_geq: "2026-05-18T00:00:00Z" datetime_leq: "2026-05-18T20:00:00Z" }
  ) { count }
}
`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS - 20h span < 1d cap\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFGraphQL_Passes1dGroupsFor7Days(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "infra/n8n/workflows/weekly.graphql", `query {
  httpRequests1dGroups(
    filter: { date_geq: "2026-05-11" date_leq: "2026-05-18" }
  ) { sum { requests } }
}
`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS - 7d span < 30d cap\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFGraphQL_Blocks1hGroupsFor7Days(t *testing.T) {
	root := t.TempDir()
	writeGQL(t, root, "queries.graphql", `query {
  httpRequests1hGroups(
    filter: { datetime_geq: "2026-05-11T00:00:00Z" datetime_leq: "2026-05-18T00:00:00Z" }
  ) { sum { requests } }
}
`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK - 7d span > 3d cap for 1hGroups\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFGraphQL_NonApplicableFileIgnored(t *testing.T) {
	root := t.TempDir()
	// .md file mentioning a query - not applicable.
	writeGQL(t, root, "README.md", `Use httpRequestsAdaptiveGroups with datetime_geq: "2026-05-11T00:00:00Z" datetime_leq: "2026-05-18T00:00:00Z"`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (markdown is not applicable)", got.Outcome)
	}
}

func TestCFGraphQL_NoTimeArgsIgnored(t *testing.T) {
	root := t.TempDir()
	// Dataset referenced but no datetime filter - can't decide, must PASS.
	writeGQL(t, root, "infra/workflows/x.graphql", `query { httpRequestsAdaptiveGroups { count } }`)
	got := CFGraphQLDatasetByWindow(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no datetime args = can't evaluate)", got.Outcome)
	}
}

func TestParseCFDatetime(t *testing.T) {
	cases := map[string]bool{
		"2026-05-18T00:00:00Z": true,
		"2026-05-18":           true,
		"not-a-time":           false,
		"":                     false,
	}
	for in, want := range cases {
		_, ok := parseCFDatetime(in)
		if ok != want {
			t.Errorf("parseCFDatetime(%q) ok=%v; want %v", in, ok, want)
		}
	}
}

func TestFriendlyDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{24 * time.Hour, "1d"},
		{3 * 24 * time.Hour, "3d"},
		{30 * 24 * time.Hour, "30d"},
		{12 * time.Hour, "12h"},
	}
	for _, c := range cases {
		got := friendlyDuration(c.in)
		if got != c.want {
			t.Errorf("friendlyDuration(%v) = %q; want %q", c.in, got, c.want)
		}
	}
}
