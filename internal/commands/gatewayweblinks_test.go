// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

const linkSHA = "cd06b2efe41142959250fa2fd1bd14ca5889c2a4"

func saveRepoWithUpstream(t *testing.T, root, repo, upstream string) {
	t.Helper()
	if err := (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: repo, UpstreamURL: upstream, Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func feedRowsFor(t *testing.T, root, repo string) string {
	t.Helper()
	vm := gateway.BuildView([]gateway.AuditRecord{{
		Time: time.Now(), Repo: repo, Refs: []string{"refs/heads/mine"}, Accept: true,
		RefUpdates: []gateway.RefUpdate{{Name: "refs/heads/mine", OldRev: strings.Repeat("0", 40), NewRev: linkSHA}},
	}}, gateway.Filter{})
	applyCommitLinks(&vm, newCommitLinker(root))
	var buf bytes.Buffer
	if err := gwTmpl.ExecuteTemplate(&buf, "rows", vm); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestFeedSHA_linksWhenUpstreamIsKnown(t *testing.T) {
	root := t.TempDir()
	saveRepoWithUpstream(t, root, "gh", "git@github.com:acme/app.git")
	body := feedRowsFor(t, root, "gh")
	if !strings.Contains(body, `<a class="gw-sha" href="https://github.com/acme/app/commit/`+linkSHA+`"`) {
		t.Errorf("feed SHA should link to the upstream commit:\n%s", body)
	}
	if strings.Contains(body, `<button type="button" class="gw-ref gw-ref-inline" aria-expanded="false" title="show file locations">refs/heads/mine <`) {
		t.Error("the SHA must sit outside the ref button, which toggles file locations")
	}
}

func TestFeedSHA_copiesWhenNoLinkCanBeWorkedOut(t *testing.T) {
	root := t.TempDir()
	saveRepoWithUpstream(t, root, "lan", "git@192.168.1.20:acme/app.git")
	body := feedRowsFor(t, root, "lan")
	if strings.Contains(body, `<a class="gw-sha"`) || !strings.Contains(body, `data-copy="`+linkSHA+`"`) {
		t.Errorf("an unmapped SSH host must copy, not link:\n%s", body)
	}

	if err := gateway.SaveWebLinks(root, gateway.WebLinks{Hosts: []gateway.WebHost{{Host: "192.168.1.20", Web: "http://192.168.1.20:3000"}}}); err != nil {
		t.Fatal(err)
	}
	if body := feedRowsFor(t, root, "lan"); !strings.Contains(body, `href="http://192.168.1.20:3000/acme/app/commit/`+linkSHA+`"`) {
		t.Errorf("a host mapping should turn the chip into a link:\n%s", body)
	}
}

func TestOverlapsPage_linksBothCommits(t *testing.T) {
	root := t.TempDir()
	saveRepoWithUpstream(t, root, "gh", "https://github.com/acme/app.git")
	_ = gateway.AppendAudit(filepath.Join(root, "gh", "audit.log"), gateway.AuditRecord{
		Time: time.Now(), Repo: "gh", Refs: []string{"refs/heads/mine"}, Accept: true,
		Overlaps: []gateway.Overlap{{Ref: "refs/heads/mine", OtherRef: "refs/heads/other", Files: []string{"a.go"}, SHA: linkSHA, OtherSHA: strings.Repeat("b", 40)}},
	})
	rec := httptest.NewRecorder()
	serveGatewayOverlaps(root, t.TempDir())(rec, httptest.NewRequest("GET", "/overlaps", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`href="https://github.com/acme/app/commit/` + linkSHA + `"`,
		`href="https://github.com/acme/app/commit/` + strings.Repeat("b", 40) + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overlaps page missing %s", want)
		}
	}
}

func TestSettingsLinksTab_previewAndSave(t *testing.T) {
	root := t.TempDir()
	saveRepoWithUpstream(t, root, "lan", "git@192.168.1.20:acme/app.git")
	h := webLinksHandlers{policyRoot: root, token: "tok"}

	post := func(hosts string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/settings/weblinks", strings.NewReader(url.Values{"hosts": {hosts}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", "tok")
		rec := httptest.NewRecorder()
		h.save(rec, req)
		return rec
	}

	if rec := post("192.168.1.20 ftp://nope"); !strings.Contains(rec.Header().Get("Location")+rec.Header().Get("HX-Redirect"), "weblinks_err=") {
		t.Errorf("an invalid line should redirect back with the error, got %d %v", rec.Code, rec.Header())
	}
	if l, _ := gateway.LoadWebLinks(root); len(l.Hosts) != 0 {
		t.Error("an invalid line must save nothing")
	}

	post("192.168.1.20 http://192.168.1.20:3000")
	if l, _ := gateway.LoadWebLinks(root); len(l.Hosts) != 1 {
		t.Fatalf("valid mapping not saved: %+v", l)
	}

	rec := httptest.NewRecorder()
	serveSettings(root, t.TempDir(), "off", true, "tok")(rec, httptest.NewRequest("GET", "/settings?tab=links", nil))
	body := rec.Body.String()
	for _, want := range []string{`name="hosts"`, "192.168.1.20 http://192.168.1.20:3000", "http://192.168.1.20:3000/acme/app/commit/&lt;sha&gt;", `class="autopr-tab active">Commit links`} {
		if !strings.Contains(body, want) {
			t.Errorf("links tab missing %q", want)
		}
	}
}

func TestRedactURL_dropsCredentials(t *testing.T) {
	if got := redactURL("https://user:secret@github.com/acme/app.git"); strings.Contains(got, "secret") || !strings.Contains(got, "github.com/acme/app.git") {
		t.Errorf("redactURL = %q", got)
	}
	if got := redactURL("git@192.168.1.20:acme/app.git"); got != "git@192.168.1.20:acme/app.git" {
		t.Errorf("scp-style URL should pass through, got %q", got)
	}
}
