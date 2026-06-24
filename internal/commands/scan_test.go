// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestCountByGroup(t *testing.T) {
	groupSets := map[string]map[string]bool{
		"web-app":            {"convention/html-seo-meta": true, "security/no-mixed-content-urls": true},
		"cf-workers-project": {"app-correctness/cf-graphql-schema-match": true},
	}
	results := []engine.CheckResult{
		{FrameID: "convention/html-seo-meta", Outcome: engine.OutcomeWarn, Hits: []engine.Hit{{}, {}, {}}},         // 3 → web-app
		{FrameID: "app-correctness/cf-graphql-schema-match", Outcome: engine.OutcomeBlock, Hits: []engine.Hit{{}}}, // 1 → cf-workers-project
		{FrameID: "convention/html-seo-meta", Outcome: engine.OutcomePass},                                         // pass → 0
		{FrameID: "security/no-mixed-content-urls", Outcome: engine.OutcomeWarn},                                   // no hits → counts as 1 → web-app
	}
	got := countByGroup(results, groupSets)
	if got["web-app"] != 4 {
		t.Errorf("web-app = %d, want 4 (3 hits + 1 hitless warn)", got["web-app"])
	}
	if got["cf-workers-project"] != 1 {
		t.Errorf("cf-workers-project = %d, want 1", got["cf-workers-project"])
	}
}

func TestRecommend_webProject(t *testing.T) {
	g, l, _ := recommend(scanSignals{HTMLCount: 5})
	if !reflect.DeepEqual(g, []string{"core", "web-app"}) {
		t.Errorf("kits = %v, want [core web-app]", g)
	}
	if len(l) != 0 {
		t.Errorf("no linters expected for a pure-HTML project, got %v", l)
	}
}

func TestRecommend_cloudflareSvelteKit(t *testing.T) {
	// Post-fix: wrangler.toml + Svelte content (shipping HTML) → cf-pages-project only,
	// no cf-workers-project. Pre-fix recommended both, mis-classifying SvelteKit on
	// CF Pages as Workers.
	g, _, _ := recommend(scanSignals{WranglerToml: true, SvelteConfig: true, SvelteCount: 8})
	want := []string{"core", "web-app", "cf-pages-project"}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("kits = %v, want %v", g, want)
	}
}

// TestRecommend_wranglerWithHTMLisCfPages is the canonical regression for the
// G1 fix - wrangler.toml + HTML files (the myapp shape) must recommend
// cf-pages-project, not cf-workers-project.
func TestRecommend_wranglerWithHTMLisCfPages(t *testing.T) {
	g, _, _ := recommend(scanSignals{WranglerToml: true, HTMLCount: 46})
	want := []string{"core", "web-app", "cf-pages-project"}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("kits = %v, want %v (wrangler+HTML = CF Pages, NOT Workers)", g, want)
	}
}

// TestRecommend_wranglerWithoutHTMLisCfWorkers covers the "pure Workers"
// case: wrangler.toml present, no HTML signal → cf-workers-project.
func TestRecommend_wranglerWithoutHTMLisCfWorkers(t *testing.T) {
	g, _, _ := recommend(scanSignals{WranglerToml: true})
	want := []string{"core", "cf-workers-project"}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("kits = %v, want %v (wrangler without HTML = Workers backend-only)", g, want)
	}
}

func TestRecommend_goModuleSuggestsLinter(t *testing.T) {
	g, l, _ := recommend(scanSignals{GoMod: true})
	if !reflect.DeepEqual(g, []string{"core"}) {
		t.Errorf("kits = %v, want [core] (no web/cf for a pure Go project)", g)
	}
	if !reflect.DeepEqual(l, []string{"go-vet"}) {
		t.Errorf("linters = %v, want [go-vet]", l)
	}
}

func TestRecommend_migrations(t *testing.T) {
	g, _, notes := recommend(scanSignals{SQLMigrations: 3})
	if !reflect.DeepEqual(g, []string{"core"}) {
		t.Errorf("kits = %v, want [core] (migrations have no kit; dashboard note instead)", g)
	}
	hasMigrationNote := false
	for _, n := range notes {
		if strings.Contains(n, "Database") && strings.Contains(n, "Migrations") {
			hasMigrationNote = true
		}
	}
	if !hasMigrationNote {
		t.Errorf("expected a dashboard migration note, got notes: %v", notes)
	}
}

func TestRecommend_emptyStillGetsCore(t *testing.T) {
	g, _, _ := recommend(scanSignals{})
	if !reflect.DeepEqual(g, []string{"core"}) {
		t.Errorf("kits = %v, want [core] (catastrophic prevention applies to any project)", g)
	}
}

// TestScan_recommendJSONFlag verifies the --recommend-json flag exists, prints
// parseable JSON, and matches the spec schema. This is the gateway
// post-receive shell-out contract; the test pins it.
func TestScan_recommendJSONFlag(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdoutForScan(t, []string{tmp, "--recommend-json"})

	var rec struct {
		ScannedAt          string                   `json:"scanned_at"`
		TreeRef            string                   `json:"tree_ref"`
		RecommendedGroups  []map[string]interface{} `json:"recommended_groups"`
		RecommendedLinters []string                 `json:"recommended_linters"`
		Dismissed          bool                     `json:"dismissed"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("not parseable JSON: %v\nOUTPUT:\n%s", err, out)
	}
	if rec.ScannedAt == "" {
		t.Errorf("scanned_at empty: %s", out)
	}
	if rec.TreeRef == "" {
		t.Errorf("tree_ref empty: %s", out)
	}
	if len(rec.RecommendedGroups) == 0 {
		t.Fatalf("no recommended groups in output: %s", out)
	}
	g := rec.RecommendedGroups[0]
	if _, ok := g["name"]; !ok {
		t.Fatalf("group missing name field: %+v", g)
	}
	if _, ok := g["would_flag"]; !ok {
		t.Fatalf("group missing would_flag field: %+v", g)
	}
	if _, ok := g["always"]; !ok {
		t.Fatalf("group missing always field: %+v", g)
	}
	// core kit must be the first entry + always=true.
	if name, _ := g["name"].(string); name != "core" {
		t.Errorf("first group name = %q, want core", name)
	}
	if always, _ := g["always"].(bool); !always {
		t.Errorf("core always = false, want true")
	}
}

// captureStdoutForScan invokes Scan with the given args and returns stdout.
// Pipes os.Stdout into a buffer so the JSON branch can be parsed by the test.
func captureStdoutForScan(t *testing.T, args []string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	rc := Scan(args)

	_ = w.Close()
	os.Stdout = old
	<-done

	if rc != 0 {
		t.Fatalf("Scan exit = %d, want 0\nOUTPUT:\n%s", rc, buf.String())
	}
	return buf.String()
}
