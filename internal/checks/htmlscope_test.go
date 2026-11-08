// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runOneFile(t *testing.T, check engine.CheckFunc, files map[string]string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		writeWeb(t, root, rel, body)
	}
	return check(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
}

const fullHead = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width"><title>T</title>`

func TestHTMLRequiredMeta_OnlyWholeShippingPages(t *testing.T) {
	pass := map[string]string{
		"feed/index.html":                   "<table><tr><td>row</td></tr></table>\n",
		"src/routes/mood/+page.svelte":      "<script>let x = 1;</script>\n<div>{x}</div>\n",
		"src/routes/about/+page.svelte":     "<svelte:head><title>About</title></svelte:head>\n<p>hi</p>\n",
		"src/app.html":                      "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width\">%sveltekit.head%</head></html>\n",
		"testdata/negatives/page.html":      "<html><head></head><body></body></html>\n",
		".superpowers/brainstorm/view.html": "<html><head></head><body></body></html>\n",
	}
	for rel, body := range pass {
		if got := runOneFile(t, HTMLRequiredMeta, map[string]string{rel: body}); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: want PASS, got %s %s", rel, got.Outcome, got.Reason)
		}
	}
	got := runOneFile(t, HTMLRequiredMeta, map[string]string{"site/index.html": "<html><head><meta charset=\"utf-8\"><title>x</title></head></html>\n"})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("whole page missing viewport should warn; got %s", got.Outcome)
	}
}

func TestHTMLSEOMeta_SkipsPagesThatOptOutOfSearch(t *testing.T) {
	pass := map[string]string{
		"demo/frames/index.html":         fullHead + `<meta name="robots" content="noindex, nofollow"></head></html>`,
		"site/404.html":                  fullHead + "</head><body>not found</body></html>",
		"src/app.html":                   "<html><head>%sveltekit.head%</head></html>",
		"src/routes/tasks/+page.svelte":  "<svelte:head><title>Tasks</title></svelte:head>",
		"partials/report.html":           "<section><h2>Report</h2></section>",
		"tests/e2e/fixtures/landing.htm": fullHead + "</head></html>",
	}
	for rel, body := range pass {
		if got := runOneFile(t, HTMLSEOMeta, map[string]string{rel: body}); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: want PASS, got %s %s", rel, got.Outcome, got.Reason)
		}
	}
	warn := map[string]string{
		"site/index.html":              fullHead + "</head><body>landing</body></html>",
		"src/routes/blog/+page.svelte": `<svelte:head><title>Blog</title><meta name="description" content="d"></svelte:head>`,
	}
	for rel, body := range warn {
		if got := runOneFile(t, HTMLSEOMeta, map[string]string{rel: body}); got.Outcome != engine.OutcomeWarn {
			t.Errorf("%s: want WARN, got %s", rel, got.Outcome)
		}
	}
}

func TestHTMLPlaceholderContent_OnlyShippedCopy(t *testing.T) {
	hugo := map[string]string{"hugo.toml": "title = 'x'\n"}
	pass := []map[string]string{
		{"docs/superpowers/plans/plan.md": "Run against http://127.0.0.1:8080/v1 and https://example.com.\n"},
		{"hugo.toml": "", "content/docs/curl.md": "Pipe: `curl https://example.com/install | sh`\n\n```\nproxy_pass http://127.0.0.1:8080;\n// FIXME(acme): x\n```\n"},
		{"tools/utm/index.html": fullHead + "</head><body><input type=\"url\" placeholder=\"https://www.example.com\">\n<script>\nconst u = 'https://example.com';\n</script>\n<pre><code>// HACK(#142): workaround\n</code></pre><p>Use <code>FIXME(acme)</code> markers.</p></body></html>\n"},
		{"docs/drawn/template.html": "<div>{{SVG1}}</div>\n"},
		{"testdata/positives/lorem.html": "<p>Lorem ipsum dolor</p>\n"},
	}
	for _, files := range pass {
		if got := runOneFile(t, HTMLPlaceholderContent, files); got.Outcome != engine.OutcomePass {
			t.Errorf("%v: want PASS, got %s %s", keys(files), got.Outcome, got.Reason)
		}
	}
	warn := []map[string]string{
		merge(hugo, map[string]string{"content/blog/post.md": "Lorem ipsum dolor sit amet.\n"}),
		{"src/content/blog/price.md": "Costs ~$XXX/month.\n"},
		{"site/index.html": fullHead + "</head><body><p>Contact us at https://example.com/contact</p></body></html>\n"},
	}
	for _, files := range warn {
		if got := runOneFile(t, HTMLPlaceholderContent, files); got.Outcome != engine.OutcomeWarn {
			t.Errorf("%v: want WARN, got %s", keys(files), got.Outcome)
		}
	}
}

func TestHTMLMarkupValid_OptionalEndTagsAndComponents(t *testing.T) {
	pass := map[string]string{
		"a.html":                         "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><title>x</title>\n<body>\n<p>one\n<p>two\n<ul><li>a<li>b</ul>\n<table><tr><td>1<td>2<tr><td>3</table>\n<div><p>inside</div>\n",
		"src/routes/r/+page.svelte":      "<div class=\"h\">\n  <ChartCanvas data={d}>\n    <Axis />\n  </ChartCanvas>\n</div>\n",
		"tests/fixtures/broken.html":     "<div><span></div>\n",
		".superpowers/brainstorm/x.html": "<div>\n",
	}
	for rel, body := range pass {
		if got := runOneFile(t, HTMLMarkupValid, map[string]string{rel: body}); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: want PASS, got %s %s", rel, got.Outcome, got.Reason)
		}
	}
	warn := map[string]string{
		"b.html": "<div><span>text</div>\n",
		"c.html": "<html><body><div class=\"container\">\n</body></html>\n",
	}
	for rel, body := range warn {
		if got := runOneFile(t, HTMLMarkupValid, map[string]string{rel: body}); got.Outcome != engine.OutcomeWarn {
			t.Errorf("%s: want WARN, got %s", rel, got.Outcome)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
