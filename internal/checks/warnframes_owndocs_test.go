// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// warnContentFrames are the WARN/INFO frames that read file content.
// Link checking is left out: a doc's relative links point into the real
// docs tree, which this test does not rebuild.
var warnContentFrames = map[string]engine.CheckFunc{
	"app-correctness/no-blanket-lint-disable":      NoBlanketLintDisable,
	"app-correctness/no-unmarked-test-skips":       NoUnmarkedTestSkips,
	"app-correctness/no-weakened-ci":               NoWeakenedCI,
	"app-correctness/no-test-special-casing":       NoTestSpecialCasing,
	"app-correctness/no-placeholder-tests":         NoPlaceholderTests,
	"app-correctness/no-noop-test-script":          NoNoopTestScript,
	"app-correctness/dynamic-env-declared":         DynamicEnvDeclared,
	"app-correctness/prefer-static-public":         PreferStaticPublic,
	"app-correctness/top-of-page-import-safety":    TopOfPageImportSafety,
	"app-correctness/cf-graphql-dataset-by-window": CFGraphQLDatasetByWindow,
	"app-correctness/cf-graphql-schema-match":      CFGraphQLSchemaMatch,
	"commands/approved-registries-only":            ApprovedRegistriesOnly,
	"database/schema-vs-code-drift":                SchemaVsCodeDrift,
	"documentation/dated-todo":                     DatedTodo,
	"encoding/no-non-printable":                    NoNonPrintable,
	"encoding/no-zero-width-in-content":            NoZeroWidthInContent,
	"network/cidr-host-bits-zero":                  CIDRHostBitsZero,
	"network/no-localhost-in-proxy-config":         NoLocalhostInProxyConfig,
	"security/cf-pages-headers-baseline":           CFPagesHeadersBaseline,
	"security/no-database-dump-in-repo":            NoDatabaseDumpInRepo,
	"security/no-homoglyph-identifiers":            NoHomoglyphIdentifiers,
	"security/no-innerHTML-user-input":             NoInnerHTMLUserInput,
	"security/no-mixed-content-urls":               NoMixedContentURLs,
	"security/no-pii-in-source":                    NoPIIInSource,
	"security/no-out-of-tree-references":           NoOutOfTreeReferences,
	"web/html-img-alt":                             HTMLImgAlt,
	"web/html-markup-valid":                        HTMLMarkupValid,
	"web/html-placeholder-content":                 HTMLPlaceholderContent,
	"web/html-required-meta":                       HTMLRequiredMeta,
	"web/html-seo-meta":                            HTMLSEOMeta,
}

const ownDocsHead = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width, initial-scale=1"><title>frame</title>` +
	`<meta name="description" content="frame reference"><link rel="canonical" href="https://nimblegate.com/docs/frames/x/">` +
	`<meta property="og:title" content="frame"><meta property="og:description" content="frame reference">` +
	`<meta property="og:image" content="https://nimblegate.com/og.png"></head><body><main>`

// renderFrameDoc turns a frame doc into the shape the docs site serves:
// fenced blocks as <pre><code>, inline code as <code>, prose as <p>, all
// HTML-escaped, one source line per output line.
func renderFrameDoc(md string) string {
	var b strings.Builder
	b.WriteString(ownDocsHead + "\n")
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if fenceOpenRegex.MatchString(line) {
			if inFence {
				b.WriteString("</code></pre>\n")
			} else {
				b.WriteString("<pre><code>\n")
			}
			inFence = !inFence
			continue
		}
		if inFence {
			b.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		parts := strings.Split(line, "`")
		for i, p := range parts {
			if i%2 == 1 && i < len(parts)-1 {
				b.WriteString("<code>" + html.EscapeString(p) + "</code>")
			} else {
				b.WriteString(html.EscapeString(p))
			}
		}
		b.WriteString("<br>\n")
	}
	if inFence {
		b.WriteString("</code></pre>\n")
	}
	b.WriteString("</main></body></html>\n")
	return b.String()
}

// TestWarnFrames_OwnDocsPass runs every WARN/INFO content frame over each
// frame's own doc, as the docs site holds it: Markdown in a Hugo content
// folder and the rendered page in public/. A frame that flags the docs
// explaining frames would flag every user's docs the same way.
func TestWarnFrames_OwnDocsPass(t *testing.T) {
	var docs []string
	_ = filepath.WalkDir("../stdlib/frames", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			docs = append(docs, p)
		}
		return nil
	})
	if len(docs) < 50 {
		t.Fatalf("found only %d frame docs", len(docs))
	}
	for _, doc := range docs {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(doc), ".md")
		root := t.TempDir()
		writeSource(t, root, "hugo.toml", "title = 'docs'\n")
		writeSource(t, root, "content/docs/frames/"+name+".md", string(raw))
		writeSource(t, root, "public/docs/frames/"+name+"/index.html", renderFrameDoc(string(raw)))
		ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()}
		for id, check := range warnContentFrames {
			if res := check(ctx); res.Outcome != engine.OutcomePass && res.Outcome != engine.OutcomeSkip {
				t.Errorf("%s doc: %s = %s: %s", name, id, res.Outcome, res.Reason)
			}
		}
	}
}
