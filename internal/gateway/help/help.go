// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package help renders page-scoped markdown documentation for the
// nimblegate dashboard sidepanel. Markdown files are embedded at
// build time; rendered HTML is cached in memory forever (the source
// can't change between binary builds, so cache invalidation is a
// non-problem).
package help

import (
	"bytes"
	"embed"
	"fmt"
	stdhtml "html"
	"net/http"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"

	"nimblegate/internal/gwicons"
)

//go:embed *.md
var contentFS embed.FS

// pageOf normalizes a request's `?page=` value to a help file basename.
// `/policy?repo=foo` → `policy`. `/` → `index`. Leading slash stripped,
// trailing slash + query string stripped, only [a-z0-9-] kept.
func pageOf(raw string) string {
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "index"
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return ""
		}
	}
	return raw
}

var (
	md    = newMarkdown()
	cache = struct {
		sync.Mutex
		m map[string]string // page basename → rendered HTML body
	}{m: make(map[string]string)}
	titleCache = struct {
		sync.Mutex
		m map[string]string // page basename → H1 title
	}{m: make(map[string]string)}
)

func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.Linkify),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
		),
	)
}

// renderPage returns (title, htmlBody, ok). When ok=false the caller
// should render the generic fallback fragment.
func renderPage(page string) (string, string, bool) {
	if page == "" {
		return "", "", false
	}
	cache.Lock()
	if got, ok := cache.m[page]; ok {
		title := titleCache.m[page]
		cache.Unlock()
		return title, got, true
	}
	cache.Unlock()

	raw, err := contentFS.ReadFile(page + ".md")
	if err != nil {
		return "", "", false
	}
	title, body := splitTitleAndBody(string(raw))
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return "", "", false
	}
	out := gwicons.Expand(buf.String())

	cache.Lock()
	cache.m[page] = out
	titleCache.m[page] = title
	cache.Unlock()
	return title, out, true
}

// splitTitleAndBody pulls the first H1 from the markdown as the page
// title, returning the rest as the body. The H1 is dropped from the
// body so the rendered fragment doesn't repeat it (the title renders
// in the panel header, not the body).
func splitTitleAndBody(src string) (string, string) {
	lines := strings.Split(src, "\n")
	var title string
	start := 0
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "# ") {
			title = strings.TrimSpace(trim[2:])
			start = i + 1
			break
		}
		break
	}
	body := strings.Join(lines[start:], "\n")
	return title, strings.TrimLeft(body, "\n")
}

// Handler returns an http.HandlerFunc that serves help fragments.
// Endpoint: GET /help?page=<route>. Returns 200 with the fragment
// HTML for known pages, or the generic fallback for unknown pages.
// Never returns 404.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		page := pageOf(r.URL.Query().Get("page"))
		title, body, ok := renderPage(page)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		if !ok {
			writeFallback(w)
			return
		}
		writeFragment(w, title, body)
	}
}

func writeFragment(w http.ResponseWriter, title, body string) {
	if title == "" {
		title = "Help"
	}
	fmt.Fprintf(w,
		`<header class="help-head"><h1>%s</h1><button class="help-close" aria-label="Close help">×</button></header><div class="help-body">%s</div>`,
		stdhtml.EscapeString(title), body,
	)
}

func writeFallback(w http.ResponseWriter) {
	w.Write([]byte(
		`<header class="help-head"><h1>Help</h1><button class="help-close" aria-label="Close help">×</button></header><div class="help-body"><p>Help for this page hasn't been written yet. See <a href="https://github.com/nimblegate/nimblegate/tree/main/docs">the docs directory</a> for now.</p></div>`,
	))
}
