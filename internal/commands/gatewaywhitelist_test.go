// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

func postWL(t *testing.T, h whitelistHandlers, form url.Values, csrf string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/policy/whitelist/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.add(rec, req)
	return rec.Code, rec.Body.String()
}

func TestWhitelistAddPreviewThenCommit(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	h := whitelistHandlers{policyRoot: root, token: "tok"}
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	form := url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"internal/x_test.go"}, "reason": {"fixture"}}

	// Preview: returns confirm panel, writes NOTHING.
	code, body := postWL(t, h, form, "tok")
	if code != 200 || !strings.Contains(body, "About to whitelist") || !strings.Contains(body, "single file") {
		t.Fatalf("preview: code=%d body=%s", code, body)
	}
	if _, err := os.Stat(wlPath); !os.IsNotExist(err) {
		t.Fatal("preview must not write the whitelist file")
	}

	// Commit: writes + receipt.
	form.Set("confirm", "1")
	code, body = postWL(t, h, form, "tok")
	if code != 200 || !strings.Contains(body, "whitelisted") {
		t.Fatalf("commit: code=%d body=%s", code, body)
	}
	if _, err := os.Stat(wlPath); err != nil {
		t.Fatalf("commit must write the whitelist file: %v", err)
	}
	if evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "whitelist-add" }); len(evs) != 1 || evs[0].Repo != "repo-a" {
		t.Fatalf("whitelist-add event: %+v", evs)
	}
}

func TestWhitelistConfirmPanelQuotesSafe(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	h := whitelistHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"a_test.go"}, "reason": {`he said "hi" \ ok`}}
	code, body := postWL(t, h, form, "tok") // preview phase (no confirm)
	if code != 200 {
		t.Fatalf("preview code=%d", code)
	}
	// The reason must be carried by a hidden input, not interpolated into an hx-vals JSON string.
	if !strings.Contains(body, `name="reason"`) {
		t.Errorf("confirm panel should carry reason via a hidden input\n%s", body)
	}
	if strings.Contains(body, `hx-vals`) && strings.Contains(body, `"reason"`) {
		t.Errorf("confirm panel must NOT put reason in hx-vals JSON\n%s", body)
	}
}

func TestWhitelistAddPatternScopeAndValidation(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	h := whitelistHandlers{policyRoot: root, token: "tok"}

	// Glob path → preview names PATTERN.
	_, body := postWL(t, h, url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"**/*_test.go"}, "reason": {"fixtures"}}, "tok")
	if !strings.Contains(body, "PATTERN") {
		t.Errorf("glob path should be flagged PATTERN: %s", body)
	}
	// Missing reason → 400.
	if code, _ := postWL(t, h, url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"x"}}, "tok"); code != 400 {
		t.Errorf("missing reason: code=%d, want 400", code)
	}
	// Unknown frame → 400.
	if code, _ := postWL(t, h, url.Values{"repo": {"repo-a"}, "frame": {"nope/nope"}, "path": {"x"}, "reason": {"r"}}, "tok"); code != 400 {
		t.Errorf("unknown frame: code=%d, want 400", code)
	}
	// Missing CSRF → 403.
	if code, _ := postWL(t, h, url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"x"}, "reason": {"r"}}, ""); code != 403 {
		t.Errorf("no csrf: code=%d, want 403", code)
	}
}

func postWLRemove(t *testing.T, h whitelistHandlers, form url.Values, csrf string, htmx bool) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/policy/whitelist/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.remove(rec, req)
	return rec, rec.Body.String()
}

func TestWhitelistRemove_previewThenCommit(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	h := whitelistHandlers{policyRoot: root, token: "tok"}
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	// Seed an entry to remove.
	if err := os.MkdirAll(filepath.Dir(wlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
[[entry]]
frame  = "security/no-private-keys-in-repo"
path   = "internal/x_test.go"
reason = "fixture keys"
`
	if err := os.WriteFile(wlPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"repo": {"repo-a"}, "frame": {"security/no-private-keys-in-repo"}, "path": {"internal/x_test.go"}}
	rec, prev := postWLRemove(t, h, form, "tok", false)
	if rec.Code != 200 || !strings.Contains(prev, "Remove whitelist entry") || !strings.Contains(prev, "confirm") {
		t.Fatalf("preview: code=%d body=%s", rec.Code, prev)
	}
	// File unchanged after preview.
	data, _ := os.ReadFile(wlPath)
	if !strings.Contains(string(data), "internal/x_test.go") {
		t.Fatalf("preview wrote to file: %s", data)
	}

	// Commit via htmx → HX-Refresh header.
	form.Set("confirm", "1")
	rec2, _ := postWLRemove(t, h, form, "tok", true)
	if rec2.Code != 200 {
		t.Fatalf("commit code=%d", rec2.Code)
	}
	if rec2.Header().Get("HX-Refresh") != "true" {
		t.Errorf("commit: HX-Refresh not set; headers=%v", rec2.Header())
	}
	// File no longer contains the entry.
	data, _ = os.ReadFile(wlPath)
	if strings.Contains(string(data), "internal/x_test.go") {
		t.Fatalf("entry not removed: %s", data)
	}
	// Event recorded.
	evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "whitelist-remove" })
	if len(evs) != 1 || evs[0].Repo != "repo-a" {
		t.Fatalf("event: %+v", evs)
	}
}

func TestWhitelistRemove_rejectsBadCSRF(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	h := whitelistHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"repo-a"}, "frame": {"security/x"}, "path": {"y"}}
	rec, _ := postWLRemove(t, h, form, "", false)
	if rec.Code != 403 {
		t.Fatalf("code=%d want 403", rec.Code)
	}
}

func TestWhitelistRemove_rejectsBadRepoName(t *testing.T) {
	root := t.TempDir()
	h := whitelistHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"../etc"}, "frame": {"security/x"}, "path": {"y"}}
	rec, _ := postWLRemove(t, h, form, "tok", false)
	if rec.Code != 400 {
		t.Fatalf("code=%d want 400", rec.Code)
	}
}
