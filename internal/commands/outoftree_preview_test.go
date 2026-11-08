// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"nimblegate/internal/gateway"
)

const outOfTreeFrame = "security/no-out-of-tree-references"

func outOfTreeWarns(res gateway.PreviewResult) []gateway.PreviewFinding {
	var out []gateway.PreviewFinding
	for _, f := range res.Warns {
		if f.Frame == outOfTreeFrame {
			out = append(out, f)
		}
	}
	return out
}

// A pushed symlink aimed outside the repo is neutralised while the tree is
// staged; the frame still reports it through the real engine, in the preview
// exactly as on push, and a whitelist entry silences it like any finding.
func TestPolicyPreview_outOfTreeSymlinkWarns(t *testing.T) {
	policyRoot, reposRoot := t.TempDir(), t.TempDir()
	if err := (gateway.FilePolicyStore{Root: policyRoot}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", Enabled: true, ProtectedRefs: []string{"refs/heads/*"}}); err != nil {
		t.Fatal(err)
	}
	if err := (gateway.FramePolicy{Enabled: []string{"git/no-bypass-pre-commit"}}).Save(policyRoot, "demo"); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	git := func(dir string, args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(work, "docs", "leak")); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "init")
	git(work, "clone", "-q", "--bare", work, filepath.Join(reposRoot, "demo.git"))

	res, _, err := repoPreview(policyRoot, reposRoot, "demo", []string{"git/no-bypass-pre-commit", outOfTreeFrame})
	if err != nil {
		t.Fatal(err)
	}
	w := outOfTreeWarns(res)
	if len(w) != 1 || w[0].File != "docs/leak" || w[0].Severity != "WARN" ||
		w[0].Label != "symlink -> /etc/passwd points outside the repository (treated as missing)" {
		t.Fatalf("candidate with the frame: warns = %+v", res.Warns)
	}
	if res.WouldReject() {
		t.Error("a WARN must not reject the push")
	}

	// The stored policy does not hold the frame: nothing reported.
	res, _, _ = repoPreview(policyRoot, reposRoot, "demo", []string{"git/no-bypass-pre-commit"})
	if w := outOfTreeWarns(res); len(w) != 0 {
		t.Fatalf("frame off, still reported: %+v", w)
	}

	h := previewHandlers{policyRoot: policyRoot, reposRoot: reposRoot, token: "tok"}
	rec := httptest.NewRecorder()
	h.whitelistThis(rec, authoringReq(t, "/policy/preview/whitelist", url.Values{
		"repo": {"demo"}, "frame": {outOfTreeFrame}, "path": {"docs/leak"}, "reason": {"known link, reviewed"},
	}, "tok"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("whitelist-this = %d %s", rec.Code, rec.Body.String())
	}
	res, _, _ = repoPreview(policyRoot, reposRoot, "demo", []string{"git/no-bypass-pre-commit", outOfTreeFrame})
	if w := outOfTreeWarns(res); len(w) != 0 {
		t.Fatalf("whitelisted link still reported: %+v", w)
	}
}
