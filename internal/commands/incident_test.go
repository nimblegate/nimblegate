// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/incident"
)

// cfIncidentsCatalog is the user's existing catalog file used as a fixture
// source. The path is intentionally outside the nimblegate repo - these tests
// validate the incident template can hold real entries the user has already
// written elsewhere. The file is read-only; tests never write to it.
const cfIncidentsCatalog = "/srv/projects/myapp-project/future-plans/cf-incidents-and-gating-frames.md"

// withTempProject sets up a temp dir with an appframes.toml so `incident`
// commands locate a project root, chdirs into it, and returns a teardown.
func withTempProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "appframes.toml"), []byte("# test project\n[frames]\nenabled = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func TestIncidentNew_CreatesFile(t *testing.T) {
	root := withTempProject(t)
	exit := Incident([]string{"new", "--title", "Test incident"})
	if exit != 0 {
		t.Fatalf("new returned %d", exit)
	}
	dir := filepath.Join(root, ".appframes", "_incidents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read _incidents: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files; want 1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "-test-incident.md") {
		t.Errorf("filename = %q; want suffix -test-incident.md", entries[0].Name())
	}
}

func TestIncidentNew_MissingTitle(t *testing.T) {
	_ = withTempProject(t)
	exit := Incident([]string{"new"})
	if exit == 0 {
		t.Error("missing --title should fail")
	}
}

func TestIncidentNew_FromBypass(t *testing.T) {
	root := withTempProject(t)
	exit := Incident([]string{
		"new",
		"--title", "Force-pushed feature branch after rebase",
		"--from-frame", "git-safety/no-force-push-main",
		"--from-reason", "CI branch, not main",
		"--from-command", "git push --force-with-lease origin feat-x",
	})
	if exit != 0 {
		t.Fatalf("new returned %d", exit)
	}
	dir := filepath.Join(root, ".appframes", "_incidents")
	incs, errs := incident.LoadFromDir(dir)
	if len(errs) != 0 || len(incs) != 1 {
		t.Fatalf("LoadFromDir: errs=%v incs=%d", errs, len(incs))
	}
	got := incs[0]
	if got.Frontmatter.Source != incident.SourceBypass {
		t.Errorf("source = %q; want bypass", got.Frontmatter.Source)
	}
	if got.Frontmatter.SourceFrame != "git-safety/no-force-push-main" {
		t.Errorf("source-frame missing in persisted frontmatter")
	}
	if !strings.Contains(got.Body, "no-force-push-main") {
		t.Errorf("body should reference the bypassed frame")
	}
}

func TestIncidentList(t *testing.T) {
	root := withTempProject(t)
	// Create two via the actual `new` command path so we test the full flow.
	_ = Incident([]string{"new", "--title", "Alpha incident", "--time-cost-hours", "2"})
	_ = Incident([]string{"new", "--title", "Beta incident"})

	r, w, _ := os.Pipe()
	prev := os.Stdout
	os.Stdout = w
	exit := Incident([]string{"list", "--json"})
	_ = w.Close()
	os.Stdout = prev
	if exit != 0 {
		t.Fatalf("list --json returned %d", exit)
	}
	data, _ := io.ReadAll(r)
	var out incidentListOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid json: %v\nraw: %s", err, string(data))
	}
	if out.Total != 2 {
		t.Errorf("total = %d; want 2", out.Total)
	}
	if out.Draft != 2 {
		t.Errorf("draft = %d; want 2", out.Draft)
	}
	if out.Source == "" {
		t.Error("source path missing")
	}
	// Sanity: items came back from the right project dir.
	for _, it := range out.Items {
		if !strings.HasPrefix(it.Path, root) {
			t.Errorf("item path %q outside test project %q", it.Path, root)
		}
	}
}

func TestIncidentPromote_HappyPath(t *testing.T) {
	root := withTempProject(t)
	if exit := Incident([]string{"new", "--title", "Migration wrong DB", "--time-cost-hours", "3"}); exit != 0 {
		t.Fatalf("new returned %d", exit)
	}
	// Now promote it.
	exit := Incident([]string{
		"promote", "migration-wrong-db",
		"--category", "commands",
		"--name", "wrangler-explicit-env",
		"--tier", "1",
		"--severity", "BLOCK",
		"--triggers", "pre-commit,cli",
	})
	if exit != 0 {
		t.Fatalf("promote returned %d", exit)
	}
	frameDst := filepath.Join(root, ".appframes", "commands", "wrangler-explicit-env.md")
	data, err := os.ReadFile(frameDst)
	if err != nil {
		t.Fatalf("frame stub not written: %v", err)
	}
	for _, want := range []string{
		"name: wrangler-explicit-env",
		"category: commands",
		"severity: BLOCK",
		"tier: 1",
		"triggers: [pre-commit, cli]",
		"commands/wrangler-explicit-env",
		"time-cost-hours-prevented: 3", // auto-filled from incident's time-cost-hours
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("frame stub missing %q\nGot:\n%s", want, string(data))
		}
	}

	// Original incident should now be status: promoted with promoted-to set.
	incs, _ := incident.LoadFromDir(filepath.Join(root, ".appframes", "_incidents"))
	if len(incs) != 1 {
		t.Fatalf("got %d incidents; want 1", len(incs))
	}
	if incs[0].Frontmatter.Status != incident.StatusPromoted {
		t.Errorf("incident status = %q; want promoted", incs[0].Frontmatter.Status)
	}
	if incs[0].Frontmatter.PromotedTo != "commands/wrangler-explicit-env" {
		t.Errorf("promoted-to = %q; want commands/wrangler-explicit-env", incs[0].Frontmatter.PromotedTo)
	}
}

func TestIncidentPromote_InvalidArgs(t *testing.T) {
	_ = withTempProject(t)
	_ = Incident([]string{"new", "--title", "Test"})

	cases := []struct {
		name string
		args []string
	}{
		{"missing slug", []string{"promote", "--category", "security", "--name", "x", "--tier", "1", "--severity", "BLOCK", "--triggers", "cli"}},
		{"bad category", []string{"promote", "test", "--category", "made-up", "--name", "x", "--tier", "1", "--severity", "BLOCK", "--triggers", "cli"}},
		{"bad tier", []string{"promote", "test", "--category", "security", "--name", "x", "--tier", "0", "--severity", "BLOCK", "--triggers", "cli"}},
		{"bad severity", []string{"promote", "test", "--category", "security", "--name", "x", "--tier", "1", "--severity", "OOPS", "--triggers", "cli"}},
		{"empty triggers", []string{"promote", "test", "--category", "security", "--name", "x", "--tier", "1", "--severity", "BLOCK", "--triggers", ""}},
		{"bad trigger", []string{"promote", "test", "--category", "security", "--name", "x", "--tier", "1", "--severity", "BLOCK", "--triggers", "bogus"}},
		{"bad name", []string{"promote", "test", "--category", "security", "--name", "-leading-dash", "--tier", "1", "--severity", "BLOCK", "--triggers", "cli"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if exit := Incident(c.args); exit == 0 {
				t.Errorf("expected non-zero exit for %s", c.name)
			}
		})
	}
}

// TestIncident_CfCatalogFixture verifies that the incident template can hold
// the same shape as a real entry from the user's cf-incidents catalog.
//
// The catalog file lives outside the nimblegate repo (in myapp-project);
// it is read-only here. If the file is missing (e.g. CI without that repo
// available), the test is skipped rather than failed - incident-template
// validity does not depend on the myapp project being present.
//
// The test extracts the section-1 headers ("Incident", "Detection signal",
// "Frame proposals", "Where the check belongs", "Generalizes to") and
// confirms our scaffolded template carries those same sections so a user
// porting an entry from the catalog finds the structure already there.
func TestIncident_CfCatalogFixture(t *testing.T) {
	src, err := os.ReadFile(cfIncidentsCatalog)
	if err != nil {
		t.Skipf("cf-incidents catalog not available at %s: %v", cfIncidentsCatalog, err)
	}
	srcText := string(src)
	// Sanity: the catalog actually has the section structure we're matching against.
	for _, header := range []string{
		"**Incident:**",
		"**Detection signal:**",
		"**Frame proposal",
		"**Where the check",
		"**Generalizes to:**",
	} {
		if !strings.Contains(srcText, header) {
			t.Fatalf("catalog fixture missing expected header %q, has the catalog format changed?", header)
		}
	}

	// Scaffold a fresh draft and confirm the same conceptual sections are
	// present in our template (modulo bold vs heading style: we use H2
	// headers, the catalog uses bold inline labels - same content shape).
	root := withTempProject(t)
	if exit := Incident([]string{"new", "--title", "Migration script silently runs against wrong DB"}); exit != 0 {
		t.Fatalf("new returned %d", exit)
	}
	dir := filepath.Join(root, ".appframes", "_incidents")
	incs, errs := incident.LoadFromDir(dir)
	if len(errs) != 0 || len(incs) != 1 {
		t.Fatalf("LoadFromDir: errs=%v len=%d", errs, len(incs))
	}
	body := incs[0].Body
	for _, want := range []string{
		"## Incident",
		"## Detection signal",
		"## Frame proposal",
		"## Where the check belongs",
		"## Generalizes to",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("template missing section %q\nbody:\n%s", want, body)
		}
	}
}
