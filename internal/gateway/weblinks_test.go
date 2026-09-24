// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
)

func TestCommitURL(t *testing.T) {
	const sha = "cd06b2efe41142959250fa2fd1bd14ca5889c2a4"
	lan := WebLinks{Hosts: []WebHost{
		{Host: "192.168.1.20", Web: "http://192.168.1.20:3000"},
		{Host: "git.corp.test", Web: "https://git.corp.test", Kind: "gitlab"},
	}}
	cases := []struct {
		name, upstream, want string
		links                WebLinks
	}{
		{"https github", "https://github.com/acme/app.git", "https://github.com/acme/app/commit/" + sha, WebLinks{}},
		{"https gitea with port", "https://gitea.test:8443/acme/app", "https://gitea.test:8443/acme/app/commit/" + sha, WebLinks{}},
		{"https gitlab.com", "https://gitlab.com/acme/app.git", "https://gitlab.com/acme/app/-/commit/" + sha, WebLinks{}},
		{"credentials never leak", "https://user:tok@github.com/acme/app.git", "https://github.com/acme/app/commit/" + sha, WebLinks{}},
		{"scp github", "git@github.com:acme/app.git", "https://github.com/acme/app/commit/" + sha, WebLinks{}},
		{"ssh bitbucket", "ssh://git@bitbucket.org/acme/app.git", "https://bitbucket.org/acme/app/commits/" + sha, WebLinks{}},
		{"scp LAN without mapping: no guess", "git@192.168.1.20:acme/app.git", "", WebLinks{}},
		{"scp LAN with mapping", "git@192.168.1.20:acme/app.git", "http://192.168.1.20:3000/acme/app/commit/" + sha, lan},
		{"ssh port uses host mapping", "ssh://git@192.168.1.20:2222/acme/app.git", "http://192.168.1.20:3000/acme/app/commit/" + sha, lan},
		{"mapping kind wins", "git@git.corp.test:team/app.git", "https://git.corp.test/team/app/-/commit/" + sha, lan},
		{"local path", "/srv/mirror/app.git", "", lan},
		{"file url", "file:///srv/mirror/app.git", "", lan},
		{"empty", "", "", lan},
	}
	for _, c := range cases {
		if got := CommitURL(c.upstream, sha, c.links); got != c.want {
			t.Errorf("%s: CommitURL(%q) = %q, want %q", c.name, c.upstream, got, c.want)
		}
	}
	if got := CommitURL("https://github.com/acme/app.git", "", WebLinks{}); got != "" {
		t.Errorf("no SHA must give no link, got %q", got)
	}
}

func TestParseWebHostLines(t *testing.T) {
	got, err := ParseWebHostLines("# my gitea\n192.168.1.20 http://192.168.1.20:3000/\n\nGit.Corp.Test https://git.corp.test GitLab\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []WebHost{{Host: "192.168.1.20", Web: "http://192.168.1.20:3000"}, {Host: "git.corp.test", Web: "https://git.corp.test", Kind: "gitlab"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
	for _, bad := range []string{
		"hostonly",
		"host http://x extra-kind",
		"host ftp://x",
		"http://host http://x",
		"host notaurl",
		"a b c d",
	} {
		if _, err := ParseWebHostLines(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		} else if !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%q: error should name the line, got %v", bad, err)
		}
	}
}

func TestWebLinks_SaveLoadRoundtrip(t *testing.T) {
	root := t.TempDir()
	if l, err := LoadWebLinks(root); err != nil || len(l.Hosts) != 0 {
		t.Fatalf("missing file should be an empty mapping: %+v, %v", l, err)
	}
	in := WebLinks{Hosts: []WebHost{{Host: "192.168.1.20", Web: "http://192.168.1.20:3000"}, {Host: "g.test", Web: "https://g.test", Kind: "bitbucket"}}}
	if err := SaveWebLinks(root, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadWebLinks(root)
	if err != nil || len(out.Hosts) != 2 || out.Hosts[1] != in.Hosts[1] {
		t.Fatalf("roundtrip: %+v, %v", out, err)
	}
	if got := out.Lines(); got != "192.168.1.20 http://192.168.1.20:3000\ng.test https://g.test bitbucket\n" {
		t.Errorf("Lines() = %q", got)
	}
}
