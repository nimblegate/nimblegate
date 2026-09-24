// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// WebHost maps a git host, as written in upstream URLs, to the base URL of its
// web UI. Needed wherever the web address cannot be read off the upstream URL:
// an SSH upstream says nothing about the web UI's scheme or port.
type WebHost struct {
	Host string `toml:"host"`           // e.g. "192.168.1.20" or "git.example.com"
	Web  string `toml:"web"`            // e.g. "http://192.168.1.20:3000"
	Kind string `toml:"kind,omitempty"` // "" (GitHub/Gitea/Forgejo style) | "gitlab" | "bitbucket"
}

// WebLinks is the operator's host mapping, stored in <policy-root>/weblinks.toml.
// Dashboard-owned like license.toml, so a dashboard save never rewrites the
// operator-edited gateway.toml.
type WebLinks struct {
	Hosts []WebHost `toml:"host"`
}

func webLinksPath(policyRoot string) string {
	return filepath.Join(policyRoot, "weblinks.toml")
}

// LoadWebLinks reads <policy-root>/weblinks.toml. A missing file is an empty
// mapping, not an error.
func LoadWebLinks(policyRoot string) (WebLinks, error) {
	data, err := os.ReadFile(webLinksPath(policyRoot))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return WebLinks{}, nil
		}
		return WebLinks{}, err
	}
	var l WebLinks
	if err := toml.Unmarshal(data, &l); err != nil {
		return WebLinks{}, err
	}
	return l, nil
}

// SaveWebLinks rewrites <policy-root>/weblinks.toml wholesale.
func SaveWebLinks(policyRoot string, l WebLinks) error {
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		return err
	}
	f, err := os.Create(webLinksPath(policyRoot))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(l)
}

// ParseWebHostLines parses the Settings textarea: one mapping per line,
// "<host> <web-url> [gitlab|bitbucket]". Blank lines and # comments are skipped.
func ParseWebHostLines(text string) ([]WebHost, error) {
	var out []WebHost
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 || len(f) > 3 {
			return nil, fmt.Errorf("line %d: want \"<host> <web-url> [gitlab|bitbucket]\", got %q", i+1, line)
		}
		h := WebHost{Host: strings.ToLower(f[0]), Web: strings.TrimRight(f[1], "/")}
		if strings.Contains(h.Host, "/") {
			return nil, fmt.Errorf("line %d: host %q must be a bare host name, without scheme or path", i+1, f[0])
		}
		if u, err := url.Parse(h.Web); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("line %d: web URL %q must start with http:// or https://", i+1, f[1])
		}
		if len(f) == 3 {
			h.Kind = strings.ToLower(f[2])
			if h.Kind != "gitlab" && h.Kind != "bitbucket" {
				return nil, fmt.Errorf("line %d: kind %q must be gitlab or bitbucket (omit it for GitHub, Gitea, Forgejo)", i+1, f[2])
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// Lines renders the mapping back into the textarea format ParseWebHostLines reads.
func (l WebLinks) Lines() string {
	var b strings.Builder
	for _, h := range l.Hosts {
		b.WriteString(h.Host + " " + h.Web)
		if h.Kind != "" {
			b.WriteString(" " + h.Kind)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// publicHosts are the hosts whose web UI is known to sit at https://<host>, so
// an SSH upstream on them links without a mapping. Value is the Kind.
var publicHosts = map[string]string{
	"github.com":    "",
	"codeberg.org":  "",
	"gitlab.com":    "gitlab",
	"bitbucket.org": "bitbucket",
}

// CommitURL returns the web page for commit sha in the repo at upstreamURL, or
// "" when no page can be worked out. A mapping for the upstream's host wins; an
// http(s) upstream otherwise links on its own address; an SSH upstream links
// only on a known public host. It never guesses: a wrong link is worse than none.
func CommitURL(upstreamURL, sha string, links WebLinks) string {
	scheme, host, hostport, path := parseUpstreamURL(upstreamURL)
	if host == "" || path == "" || sha == "" {
		return ""
	}
	var base, kind string
	if h, ok := links.lookup(host, hostport); ok {
		base, kind = h.Web, h.Kind
	} else if scheme == "http" || scheme == "https" {
		base, kind = scheme+"://"+hostport, publicHosts[host]
	} else if k, ok := publicHosts[host]; ok {
		base, kind = "https://"+host, k
	} else {
		return ""
	}
	segment := "/commit/"
	switch kind {
	case "gitlab":
		segment = "/-/commit/"
	case "bitbucket":
		segment = "/commits/"
	}
	return base + "/" + path + segment + sha
}

func (l WebLinks) lookup(host, hostport string) (WebHost, bool) {
	for _, h := range l.Hosts {
		if strings.EqualFold(h.Host, host) || strings.EqualFold(h.Host, hostport) {
			return h, true
		}
	}
	return WebHost{}, false
}

// parseUpstreamURL splits an upstream URL into scheme, host, host[:port] and
// the owner/repo path without ".git". Credentials are never part of the
// result. Local paths and file:// upstreams return an empty host.
func parseUpstreamURL(u string) (scheme, host, hostport, path string) {
	switch {
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "ssh://"):
		pu, err := url.Parse(u)
		if err != nil {
			return "", "", "", ""
		}
		scheme, host, hostport, path = pu.Scheme, strings.ToLower(pu.Hostname()), strings.ToLower(pu.Host), pu.Path
	case !strings.Contains(u, "://") && !strings.HasPrefix(u, "/") && strings.Contains(u, ":"):
		// scp-like: [user@]host:owner/repo.git
		i := strings.Index(u, ":")
		hp := u[:i]
		if at := strings.LastIndex(hp, "@"); at >= 0 {
			hp = hp[at+1:]
		}
		scheme, host, hostport, path = "scp", strings.ToLower(hp), strings.ToLower(hp), u[i+1:]
	default:
		return "", "", "", ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	return scheme, host, hostport, path
}
