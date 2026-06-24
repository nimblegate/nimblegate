// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

func seedReposTestRepo(t *testing.T, policyRoot, reposRoot, name, upstream string) {
	t.Helper()
	if err := gateway.AddRepo(gateway.AddOptions{
		Name:        name,
		UpstreamURL: upstream,
		Enabled:     true,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     "/bin/true",
	}); err != nil {
		t.Fatalf("seedReposTestRepo %s: %v", name, err)
	}
}

func renderReposBody(t *testing.T, opts reposPageOpts) string {
	t.Helper()
	var buf bytes.Buffer
	if err := renderReposPage(&buf, opts); err != nil {
		t.Fatalf("renderReposPage: %v", err)
	}
	return buf.String()
}

func TestReposPage_ShowsAllRegisteredRepos(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "alpha", "https://git.example.com/alpha.git")
	seedReposTestRepo(t, policyRoot, reposRoot, "beta", "https://git.example.com/beta.git")

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})

	for _, want := range []string{
		"alpha",
		"beta",
		"https://git.example.com/alpha.git",
		"https://git.example.com/beta.git",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repos page missing %q", want)
		}
	}
}

func TestReposPage_AddFormPresent(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "demo", "https://git.example.com/demo.git")

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})

	for _, want := range []string{
		`name="name"`,
		`name="upstream"`,
		"gw-status-fieldset",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("add-repo form missing %q", want)
		}
	}
}

func TestReposPage_EmptyState(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	_ = os.MkdirAll(policyRoot, 0o755)

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})

	if !strings.Contains(body, "No repos registered yet") {
		t.Error("empty state notice missing")
	}
	// Add form opens by default when no repos.
	if !strings.Contains(body, `open`) {
		t.Error("add form should be open by default when no repos")
	}
}

func TestReposPage_CredentialRotationSection(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "myrepo", "https://git.example.com/myrepo.git")

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})

	// Credential rotation now lives in its own collapsible section below the
	// active repos table, with a <select name="repo"> picker instead of a
	// per-row form. Same endpoint, same field names.
	for _, want := range []string{
		"Add or rotate upstream credential",
		"/policy/repo/credential",
		`name="upstream_credential"`,
		`name="repo"`,
		"myrepo",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("credential rotation section missing %q", want)
		}
	}
}

func TestReposPage_CredentialSetBadge(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "badgerepo", "https://git.example.com/badgerepo.git")

	// Without credential file: unset badge.
	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})
	if !strings.Contains(body, "credential unset") {
		t.Error("expected 'credential unset' badge when credential file absent")
	}
	if strings.Contains(body, "credential set") {
		t.Error("'credential set' badge must not appear when credential file absent")
	}

	// Write credential file: set badge.
	credPath := filepath.Join(policyRoot, "badgerepo", "credential")
	if err := os.WriteFile(credPath, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	body2 := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})
	if !strings.Contains(body2, "credential set") {
		t.Error("expected 'credential set' badge when credential file present")
	}
}

func TestReposPage_ArchivedPanelListsArchived(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)

	seedReposTestRepo(t, policyRoot, reposRoot, "archived-repo", "https://git.example.com/archived-repo.git")
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{
		Name: "archived-repo", PolicyRoot: policyRoot, ReposRoot: reposRoot,
	}); err != nil {
		t.Fatalf("ArchiveRepo: %v", err)
	}
	seedReposTestRepo(t, policyRoot, reposRoot, "active-repo", "https://git.example.com/active-repo.git")

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
	})

	if !strings.Contains(body, "archived-repo") {
		t.Error("archived panel missing archived-repo")
	}
	if !strings.Contains(body, "/policy/repo/restore") {
		t.Error("archived panel missing restore form")
	}
	// Active repo appears in the table, not the archived panel.
	if !strings.Contains(body, "active-repo") {
		t.Error("active repo missing from repos table")
	}
}
