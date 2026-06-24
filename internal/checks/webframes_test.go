// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeWeb(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- html-required-meta ---

func TestHTMLRequiredMeta_WarnsOnMissingAll(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", "<html><body><p>hello</p></body></html>")
	got := HTMLRequiredMeta(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN", got.Outcome)
	}
	for _, want := range []string{"charset", "viewport", "title"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("missing %q in reason: %s", want, got.Reason)
		}
	}
}

func TestHTMLRequiredMeta_PassesWithAll(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>OK</title>
</head><body></body></html>`)
	got := HTMLRequiredMeta(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestHTMLRequiredMeta_SvelteHeadSatisfiesTitle(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "src/routes/+page.svelte", `<svelte:head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</svelte:head>
<h1>Page</h1>
`)
	got := HTMLRequiredMeta(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (svelte:head present implies title is set dynamically)\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- html-seo-meta ---

func TestHTMLSEOMeta_WarnsOnMissing(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><head><title>X</title></head></html>`)
	got := HTMLSEOMeta(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
	for _, want := range []string{"description", "canonical", "og:title", "og:description", "og:image"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("missing %q in reason: %s", want, got.Reason)
		}
	}
}

func TestHTMLSEOMeta_PassesWithFullSet(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><head>
<title>X</title>
<meta name="description" content="d">
<link rel="canonical" href="https://example.com/">
<meta property="og:title" content="X">
<meta property="og:description" content="d">
<meta property="og:image" content="https://example.com/og.png">
</head></html>`)
	got := HTMLSEOMeta(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- html-img-alt ---

func TestHTMLImgAlt_WarnsOnMissingAlt(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><body><img src="/photo.jpg"></body></html>`)
	got := HTMLImgAlt(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestHTMLImgAlt_PassesWithEmptyAlt(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><body><img src="/divider.svg" alt=""></body></html>`)
	got := HTMLImgAlt(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (alt=\"\" is intentional/decorative)", got.Outcome)
	}
}

func TestHTMLImgAlt_PassesWithRealAlt(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><body><img alt="construction site" src="/photo.jpg"></body></html>`)
	got := HTMLImgAlt(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- html-markup-valid ---

func TestHTMLMarkupValid_FlagsUnclosedTag(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<section>
  <p>hello
</section>`)
	got := HTMLMarkupValid(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "<p>") {
		t.Errorf("expected <p> mention in reason; got: %s", got.Reason)
	}
}

func TestHTMLMarkupValid_FlagsDuplicateID(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<input id="email">
<input id="email">`)
	got := HTMLMarkupValid(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "duplicate id") {
		t.Errorf("expected duplicate id in reason; got: %s", got.Reason)
	}
}

func TestHTMLMarkupValid_PassesOnCleanHTML(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<html><head><title>x</title></head><body>
<section><p>hello</p></section>
<img src="/x" alt="">
<input id="a">
<input id="b">
</body></html>`)
	got := HTMLMarkupValid(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- html-placeholder-content ---

func TestHTMLPlaceholderContent_FlagsLoremIpsum(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<p>Lorem ipsum dolor sit amet.</p>`)
	got := HTMLPlaceholderContent(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestHTMLPlaceholderContent_FlagsLocalhostURL(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<a href="http://localhost:8080">dev link</a>`)
	got := HTMLPlaceholderContent(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestHTMLPlaceholderContent_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.md", `Run with:
<!-- appframes:disable-next-line web/html-placeholder-content -->
`+"`curl http://localhost:8080/`")
	got := HTMLPlaceholderContent(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- no-mixed-content-urls ---

func TestNoMixedContentURLs_BlocksHttpInSrc(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<img src="http://cdn.example.com/logo.png" alt="">`)
	// Note: cdn.example.com is NOT in the exempt list (only example.com itself
	// without subdomain wildcarding matches at the regex level - let's check).
	got := NoMixedContentURLs(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (cdn.example.com is not exempt)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoMixedContentURLs_PassesWithHttps(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<img src="https://cdn.example.com/logo.png" alt="">`)
	got := NoMixedContentURLs(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoMixedContentURLs_ExemptsXMLNamespace(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<svg xmlns="http://www.w3.org/2000/svg" width="10"><rect/></svg>`)
	// xmlns isn't src/href so wouldn't fire anyway, but content URLs like
	// xlink:href would. Add one to test the regex matches and exempts.
	writeWeb(t, root, "page2.html", `<a href="http://www.w3.org/2000/svg">spec</a>`)
	got := NoMixedContentURLs(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (w3.org is an exempt schema host)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestNoMixedContentURLs_ExemptsLocalhost(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "page.html", `<a href="http://localhost:8080/api">dev</a>`)
	got := NoMixedContentURLs(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (localhost exempt)\nreason: %s", got.Outcome, got.Reason)
	}
}

// --- cf-pages-headers-baseline ---

func TestCFPagesHeadersBaseline_PassesWithoutHeadersFile(t *testing.T) {
	root := t.TempDir()
	got := CFPagesHeadersBaseline(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no _headers = out of scope)", got.Outcome)
	}
}

func TestCFPagesHeadersBaseline_WarnsOnMissingHeaders(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "_headers", `/*
  Cache-Control: no-store
`)
	got := CFPagesHeadersBaseline(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN\nreason: %s", got.Outcome, got.Reason)
	}
	for _, want := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Strict-Transport-Security"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("missing %q in reason: %s", want, got.Reason)
		}
	}
}

func TestCFPagesHeadersBaseline_PassesWithFullBaseline(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "_headers", `/*
  Content-Security-Policy: default-src 'self'
  X-Frame-Options: DENY
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  Strict-Transport-Security: max-age=31536000; includeSubDomains
`)
	got := CFPagesHeadersBaseline(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCFPagesHeadersBaseline_FileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "_headers", `# appframes:disable security/cf-pages-headers-baseline
# Headers set in hooks.server.ts; _headers handles only cache.

/static/*
  Cache-Control: public, max-age=31536000, immutable
`)
	got := CFPagesHeadersBaseline(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file-disabled)\nreason: %s", got.Outcome, got.Reason)
	}
}
