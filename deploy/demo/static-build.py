#!/usr/bin/env python3
"""Snapshot a running read-only demo dashboard into a pure-static CF Pages site.

Crawls every internal link from the nav (including ?tab= / ?id= / ?repo=
query-param links that pure static can't route), saves each to a path-based
file, and rewrites all internal links to those paths - so tabs, frame-detail
pages, and per-repo views all click through with no server behind them.

  python3 static-build.py [BASE_URL] [OUT_DIR]
"""
import os, re, sys, shutil
from urllib.parse import unquote
from urllib.request import urlopen

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:7902").rstrip("/")
OUT = sys.argv[2] if len(sys.argv) > 2 else "deploy/demo-static"
SEEDS = ["/", "/feed", "/repos", "/frames", "/events", "/stats", "/health",
         "/auto-pr", "/auto-pr/config", "/policy", "/settings", "/ssh-keys"]
ASSETS = ["/static/gwshell.js", "/static/htmx.min.js", "/static/favicon.svg"]
MAX_PAGES = 200

BANNER = ('<style>body{padding-top:34px!important}'
          # hide controls that can't work on a static snapshot - the help
          # sidepanel (JS toggle + /help?page= fetch: opens stuck, no content):
          '.gw-help-toggle,.gw-help{display:none!important}'
          '.nbg-demo-bar{position:fixed;'
          'top:0;left:0;right:0;z-index:99999;background:#11304d;color:#cfe4ff;'
          'font:13px/34px -apple-system,Segoe UI,Roboto,sans-serif;text-align:center;'
          'border-bottom:1px solid #1e4060}</style>'
          '<div class="nbg-demo-bar">Visual demo - a read-only snapshot, no live '
          'backend. <a href="https://github.com/nimblegate/nimblegate" '
          'style="color:#79c0ff">Install nimblegate</a> to run it on your own repos.</div>')

# Share-preview + social meta injected into every snapshot page's <head>.
# Deliberately noindex (the demo is a backend-less snapshot; it should not
# compete with nimblegate.com in search) - the OG/Twitter tags are purely for
# nice link previews when the demo URL is shared. robots.txt also Disallows.
DEMO_DESC = ("Click through a real nimblegate dashboard over sample data: the live "
             "feed, stats, policy, and Auto-PR fix-loop. No backend, nothing to install.")
META = (
    '<meta name="robots" content="noindex, nofollow">'
    f'<meta name="description" content="{DEMO_DESC}">'
    '<meta name="theme-color" content="#0f1115">'
    '<meta property="og:type" content="website">'
    '<meta property="og:site_name" content="nimblegate">'
    '<meta property="og:url" content="https://demo.nimblegate.com/">'
    '<meta property="og:title" content="nimblegate demo: read-only dashboard">'
    f'<meta property="og:description" content="{DEMO_DESC}">'
    '<meta property="og:image" content="https://demo.nimblegate.com/og.png">'
    '<meta name="twitter:card" content="summary_large_image">'
    '<meta name="twitter:title" content="nimblegate demo: read-only dashboard">'
    f'<meta name="twitter:description" content="{DEMO_DESC}">'
    '<meta name="twitter:image" content="https://demo.nimblegate.com/og.png">'
)

href_re = re.compile(r'(href|hx-get)="(/[^"]*)"')

# Controls that need a live server/JS and can't work on a static snapshot:
# the repo-switch <select>s (onchange navigates to ?repo= URLs that don't exist
# as files). Strip the inline onchange and disable the selects so they're inert
# rather than navigating to 404s. (The help sidepanel is hidden via injected
# CSS in BANNER - more robust than excising its markup.)
onchange_re = re.compile(r'\sonchange="[^"]*"')

def neuter(html):
    html = onchange_re.sub("", html)
    html = html.replace("<select ", "<select disabled ")
    return html

def relpath(link):
    """Map an internal link (/path?query) to a static dir path (no leading/trailing /)."""
    link = link.replace("&amp;", "&")
    path, _, q = link.partition("?")
    parts = [p for p in path.strip("/").split("/") if p]
    if q:
        parts.append(re.sub(r"[^a-z0-9]+", "-", unquote(q).lower()).strip("-"))
    return "/".join(parts)

def static_href(link):
    rel = relpath(link)
    return "/" + rel + "/" if rel else "/"

def fetch(url):
    with urlopen(url, timeout=10) as r:
        return r.read()

def main():
    if os.path.isdir(OUT):
        shutil.rmtree(OUT)
    os.makedirs(os.path.join(OUT, "static"), exist_ok=True)

    for a in ASSETS:
        try:
            with open(os.path.join(OUT, a.lstrip("/")), "wb") as fh:
                fh.write(fetch(BASE + a))
        except Exception as e:
            print(f"  warn: asset {a}: {e}", file=sys.stderr)

    seen, queue = set(), list(SEEDS)
    while queue and len(seen) < MAX_PAGES:
        link = queue.pop(0)
        key = link.replace("&amp;", "&")
        if key in seen:
            continue
        seen.add(key)
        try:
            html = fetch(BASE + link).decode("utf-8", "replace")
        except Exception as e:
            print(f"  warn: {link}: {e}", file=sys.stderr)
            continue
        # enqueue internal links (skip assets / fragments / external)
        for _, target in href_re.findall(html):
            if target.startswith("/static/") or target == "/" and link == "/":
                continue
            if target.replace("&amp;", "&") not in seen:
                queue.append(target)
        # rewrite internal links to static paths
        def sub(m):
            attr, target = m.group(1), m.group(2)
            if target.startswith("/static/"):
                return m.group(0)
            return f'{attr}="{static_href(target)}"'
        html = href_re.sub(sub, html)
        html = neuter(html)
        if "</body>" in html and "nbg-demo-bar" not in html:
            html = html.replace("</body>", BANNER + "</body>", 1)
        if "og:image" not in html and "</title>" in html:
            html = html.replace("</title>", "</title>" + META, 1)
        rel = relpath(link)
        dest = os.path.join(OUT, rel, "index.html") if rel else os.path.join(OUT, "index.html")
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        with open(dest, "w", encoding="utf-8") as fh:
            fh.write(html)

    n = sum(1 for _, _, fs in os.walk(OUT) for f in fs if f == "index.html")
    print(f"captured {n} pages → {OUT}")

if __name__ == "__main__":
    main()
