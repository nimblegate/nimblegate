// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package incident

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"":                               "incident",
		"Migration ran against wrong DB": "migration-ran-against-wrong-db",
		"   spaces & punctuation!!! ":    "spaces-punctuation",
		"UPPERCASE":                      "uppercase",
		"$env/dynamic/public crashes on undefined": "env-dynamic-public-crashes-on-undefined",
		strings.Repeat("a", 80):                    strings.Repeat("a", 60),
		"with----many-----dashes":                  "with-many-dashes",
		"127.0.0.1 vs localhost on cloudflared":    "127-0-0-1-vs-localhost-on-cloudflared",
	}
	for in, want := range cases {
		got := Slugify(in)
		if got != want {
			t.Errorf("Slugify(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestFilename(t *testing.T) {
	d := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	got := Filename(d, "my-incident")
	want := "2026-05-18-my-incident.md"
	if got != want {
		t.Errorf("Filename = %q; want %q", got, want)
	}
}

func TestNewDraft_Manual(t *testing.T) {
	d := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	inc := NewDraft(NewDraftOptions{
		Title:         "Migration ran against wrong DB",
		Date:          d,
		TimeCostHours: 3,
		Tags:          []string{"db", "wrangler"},
	})
	if inc.Frontmatter.Status != StatusDraft {
		t.Errorf("status = %q; want draft", inc.Frontmatter.Status)
	}
	if inc.Frontmatter.Source != SourceManual {
		t.Errorf("source = %q; want manual", inc.Frontmatter.Source)
	}
	if inc.Frontmatter.Date != "2026-05-18" {
		t.Errorf("date = %q; want 2026-05-18", inc.Frontmatter.Date)
	}
	if !strings.Contains(inc.Body, "Migration ran against wrong DB") {
		t.Errorf("body missing title substitution: %s", inc.Body)
	}
	for _, want := range []string{"## Incident", "## Detection signal", "## Frame proposal", "## Where the check belongs", "## Generalizes to"} {
		if !strings.Contains(inc.Body, want) {
			t.Errorf("body missing section %q", want)
		}
	}
}

func TestNewDraft_Bypass(t *testing.T) {
	inc := NewDraft(NewDraftOptions{
		Title:         "force-pushed feature branch after rebase",
		Source:        SourceBypass,
		SourceFrame:   "git-safety/no-force-push-main",
		SourceReason:  "test branch in CI, not main",
		SourceCommand: "git push --force-with-lease origin feat-x",
	})
	if inc.Frontmatter.Source != SourceBypass {
		t.Errorf("source = %q; want bypass", inc.Frontmatter.Source)
	}
	if inc.Frontmatter.SourceFrame != "git-safety/no-force-push-main" {
		t.Errorf("source-frame missing")
	}
	if !strings.Contains(inc.Body, "git-safety/no-force-push-main") {
		t.Errorf("body should include bypassed frame reference")
	}
	if !strings.Contains(inc.Body, "test branch in CI, not main") {
		t.Errorf("body should include reason")
	}
	if strings.Count(inc.Body, "# force-pushed feature branch") != 1 {
		t.Errorf("body should have exactly one H1 title (got body:\n%s)", inc.Body)
	}
}

func TestRoundTrip(t *testing.T) {
	d := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	original := NewDraft(NewDraftOptions{
		Title:         "Test round trip",
		Date:          d,
		TimeCostHours: 1.5,
		Tags:          []string{"a", "b"},
	})
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := Parse(strings.NewReader(string(data)), "/tmp/test.md")
	if err != nil {
		t.Fatalf("parse: %v\nraw:\n%s", err, string(data))
	}
	if parsed.Frontmatter.Title != original.Frontmatter.Title {
		t.Errorf("title mismatch: %q vs %q", parsed.Frontmatter.Title, original.Frontmatter.Title)
	}
	if parsed.Frontmatter.TimeCostHours != 1.5 {
		t.Errorf("time-cost-hours = %v; want 1.5", parsed.Frontmatter.TimeCostHours)
	}
	if len(parsed.Frontmatter.Tags) != 2 || parsed.Frontmatter.Tags[0] != "a" {
		t.Errorf("tags = %v; want [a b]", parsed.Frontmatter.Tags)
	}
	if parsed.Frontmatter.Status != StatusDraft {
		t.Errorf("status round-trip failed: %q", parsed.Frontmatter.Status)
	}
}

func TestLoadFromDir_Empty(t *testing.T) {
	dir := t.TempDir()
	incs, errs := LoadFromDir(filepath.Join(dir, "_incidents"))
	if len(errs) != 0 {
		t.Errorf("missing dir should return no errors, got %v", errs)
	}
	if len(incs) != 0 {
		t.Errorf("missing dir should return no incidents, got %d", len(incs))
	}
}

func TestLoadFromDir_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	for i, title := range []string{"alpha-incident", "beta-incident", "gamma-incident"} {
		inc := NewDraft(NewDraftOptions{
			Title: title,
			Date:  time.Date(2026, 5, 18-i, 0, 0, 0, 0, time.UTC),
		})
		fn := Filename(time.Date(2026, 5, 18-i, 0, 0, 0, 0, time.UTC), Slugify(title))
		inc.SourcePath = filepath.Join(dir, fn)
		data, _ := inc.Marshal()
		if err := os.WriteFile(inc.SourcePath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	incs, errs := LoadFromDir(dir)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(incs) != 3 {
		t.Fatalf("got %d incidents; want 3", len(incs))
	}
	// Should be sorted by filename - gamma is oldest (2026-05-16), alpha newest.
	if incs[0].Frontmatter.Title != "gamma-incident" {
		t.Errorf("first incident = %q; want gamma-incident", incs[0].Frontmatter.Title)
	}
}

func TestLoadFromDir_OneBadFileDoesNotBlockOthers(t *testing.T) {
	dir := t.TempDir()
	good := NewDraft(NewDraftOptions{Title: "good", Date: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)})
	good.SourcePath = filepath.Join(dir, "2026-05-18-good.md")
	data, _ := good.Marshal()
	_ = os.WriteFile(good.SourcePath, data, 0o644)

	bad := filepath.Join(dir, "2026-05-17-broken.md")
	_ = os.WriteFile(bad, []byte("no frontmatter here just markdown\n"), 0o644)

	incs, errs := LoadFromDir(dir)
	if len(incs) != 1 {
		t.Errorf("got %d incidents; want 1 (broken file should be skipped, not block)", len(incs))
	}
	if len(errs) != 1 {
		t.Errorf("got %d errors; want 1", len(errs))
	}
}

func TestFindBySlug(t *testing.T) {
	dir := t.TempDir()
	inc := NewDraft(NewDraftOptions{Title: "Migration wrong DB", Date: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)})
	inc.SourcePath = filepath.Join(dir, "2026-05-18-migration-wrong-db.md")
	data, _ := inc.Marshal()
	_ = os.WriteFile(inc.SourcePath, data, 0o644)

	found, err := FindBySlug(dir, "migration-wrong-db")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if found.Frontmatter.Title != "Migration wrong DB" {
		t.Errorf("found title = %q; want Migration wrong DB", found.Frontmatter.Title)
	}

	if _, err := FindBySlug(dir, "no-such-slug"); err == nil {
		t.Error("expected error for unknown slug")
	}
}

func TestIncident_Slug(t *testing.T) {
	cases := map[string]string{
		"/repo/.appframes/_incidents/2026-05-18-my-incident.md": "my-incident",
		"/repo/.appframes/_incidents/2026-01-01-foo.md":         "foo",
		"/repo/.appframes/_incidents/no-date-prefix.md":         "no-date-prefix",
	}
	for path, want := range cases {
		inc := Incident{SourcePath: path}
		if got := inc.Slug(); got != want {
			t.Errorf("Slug(%q) = %q; want %q", path, got, want)
		}
	}
}
