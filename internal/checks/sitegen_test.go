// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

const fullPage = `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>t</title>
<meta name="description" content="d"><link rel="canonical" href="https://example.com/">
<meta property="og:title" content="t"><meta property="og:description" content="d">
<meta property="og:image" content="https://example.com/og.png"></head><body><p>hi</p></body></html>`

func cliCtx(root string) engine.CheckContext {
	return engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()}
}

// hugoSite lays out a Hugo-shaped site: partials that are not whole pages,
// and a built page in public/ that carries every required tag.
func hugoSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeWeb(t, root, "hugo.toml", "baseURL = 'https://example.com/'\n")
	writeWeb(t, root, "layouts/partials/head.html", `<head><meta charset="utf-8">`)
	writeWeb(t, root, "layouts/home.html", `{{ define "main" }}<main><p>hi</p></main>{{ end }}`)
	writeWeb(t, root, "public/index.html", fullPage)
	return root
}

func TestPageChecks_QuietOnHugoFragments(t *testing.T) {
	root := hugoSite(t)
	for name, check := range map[string]func(engine.CheckContext) engine.CheckResult{
		"required-meta": HTMLRequiredMeta,
		"seo-meta":      HTMLSEOMeta,
		"markup-valid":  HTMLMarkupValid,
	} {
		if got := check(cliCtx(root)); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: outcome = %s; want PASS on template fragments\nreason: %s", name, got.Outcome, got.Reason)
		}
	}
}

func TestPageChecks_StillCheckBuiltPages(t *testing.T) {
	root := hugoSite(t)
	writeWeb(t, root, "public/bad.html", "<html><body><p>no head</p></body></html>")
	got := HTMLRequiredMeta(cliCtx(root))
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s; want WARN for a built page missing its meta", got.Outcome)
	}
	if !strings.Contains(got.Reason, "bad.html") || strings.Contains(got.Reason, "layouts") {
		t.Errorf("reason should name public/bad.html only: %s", got.Reason)
	}
}

func TestPageChecks_LayoutsWithoutConfigStillChecked(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "layouts/page.html", "<html><body><p>a real page</p></body></html>")
	if got := HTMLRequiredMeta(cliCtx(root)); got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN: a layouts/ folder with no Hugo config holds real pages", got.Outcome)
	}
	writeWeb(t, root, "layouts/open.html", "<div><p>unclosed")
	if got := HTMLMarkupValid(cliCtx(root)); got.Outcome == engine.OutcomePass {
		t.Errorf("markup-valid: want a finding outside a Hugo site, got PASS")
	}
}

func TestPageChecks_QuietOnJekyllIncludes(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "_config.yml", "title: site\n")
	writeWeb(t, root, "_includes/head.html", `<head><meta charset="utf-8">`)
	writeWeb(t, root, "_layouts/default.html", "<body>{{ content }}</body>")
	if got := HTMLRequiredMeta(cliCtx(root)); got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS on Jekyll fragments\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestTemplateFragment_RelativePath(t *testing.T) {
	root := hugoSite(t)
	if !htmlTemplateFragment(engine.CheckContext{ProjectRoot: root}, "layouts/partials/head.html") {
		t.Error("relative path under layouts/ beside hugo.toml should be a fragment")
	}
	if htmlTemplateFragment(engine.CheckContext{ProjectRoot: root}, "public/index.html") {
		t.Error("public/index.html is a page, not a fragment")
	}
}

func TestPlaceholderContent_SkipsRepoDocsNotPages(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "README.md", "Preview: `npx wrangler pages dev` then open http://localhost:8788\n")
	writeWeb(t, root, "docs/CHANGELOG.md", "- local preview on http://localhost:8788\n")
	if got := HTMLPlaceholderContent(cliCtx(root)); got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS for repo docs\nreason: %s", got.Outcome, got.Reason)
	}
	writeWeb(t, root, "docs/guide.html", `<a href="http://localhost:8080">dev</a>`)
	if got := HTMLPlaceholderContent(cliCtx(root)); got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN for a served page with a localhost URL", got.Outcome)
	}
}

func TestMarkdownLinkCheck_HugoSiteLinksAreAddresses(t *testing.T) {
	root := hugoSite(t)
	writeMD(t, root, "content/blog/post.md", "See [how it works](/how-it-works) and [the post](/blog/other/).\n")
	if got := MarkdownLinkCheckInternal(cliCtx(root)); got.Outcome != engine.OutcomePass {
		t.Fatalf("outcome = %s; want PASS for site addresses in Hugo content\nreason: %s", got.Outcome, got.Reason)
	}
	writeMD(t, root, "content/blog/broken.md", "See [missing](./nope.md).\n")
	if got := MarkdownLinkCheckInternal(cliCtx(root)); got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN: relative links in Hugo content are still checked", got.Outcome)
	}
}

func TestMarkdownLinkCheck_RootLinksOutsideHugoStillChecked(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "content/post.md", "See [gone](/nope.md).\n")
	if got := MarkdownLinkCheckInternal(cliCtx(root)); got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s; want WARN: without a Hugo config, /nope.md is a repo path", got.Outcome)
	}
}
