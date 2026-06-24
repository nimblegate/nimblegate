// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
)

// TestCollectAutoPR_listsSymlinkedRepos guards the regression where Auto-PR
// walked policyRoot with os.ReadDir + DirEntry.IsDir() - which is Lstat-based
// and false for the activation symlinks every registered repo is - so no
// registered repo ever appeared on the page. Build AddRepo's real layout (a
// _repos/<name> lib dir with gateway.toml, plus a <name> symlink to it) and
// assert the repo shows up.
func TestCollectAutoPR_listsSymlinkedRepos(t *testing.T) {
	policyRoot := t.TempDir()
	lib := filepath.Join(policyRoot, "_repos", "gw")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "gateway.toml"),
		[]byte("repo = \"gw\"\nupstream-url = \"https://example/gw.git\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "gw"), filepath.Join(policyRoot, "gw")); err != nil {
		t.Fatal(err)
	}

	d := collectAutoPR(policyRoot, time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC))
	if !d.HasAny {
		t.Fatal("HasAny = false; expected the symlinked repo to be listed")
	}
	found := false
	for _, r := range d.Repos {
		if r.Name == "gw" {
			found = true
		}
	}
	if !found {
		t.Errorf("collectAutoPR did not list symlinked repo 'gw'; got %+v", d.Repos)
	}

	// The Setup-tab dropdown lister must agree.
	names := listConfiguredRepos(policyRoot)
	if len(names) != 1 || names[0] != "gw" {
		t.Errorf("listConfiguredRepos = %v; want [gw]", names)
	}
}

func TestAutoPRRetryHandler_resetsBackoffAndRequeues(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	qPath := filepath.Join(policyRoot, "foo", "pr-comment-queue.jsonl")
	dlPath := filepath.Join(policyRoot, "foo", "pr-comment-deadletter.jsonl")
	_ = notification.AppendQueueRecord(qPath, notification.QueueRecord{ID: "q1", DeliveryAttempts: 5, LastError: "HTTP 403", NextRetryAt: time.Unix(9999999999, 0)})
	_ = notification.AppendQueueRecord(dlPath, notification.QueueRecord{ID: "d1", DeliveryAttempts: 20, LastError: "HTTP 403"})

	h := autoPRRetryHandler(policyRoot, true, func() string { return "tok" })
	req := httptest.NewRequest("POST", "/auto-pr/retry", strings.NewReader(url.Values{"repo": {"foo"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if dl, _ := notification.ReadQueueRecords(dlPath); len(dl) != 0 {
		t.Fatalf("deadletter should be empty after requeue, got %d", len(dl))
	}
	q, _ := notification.ReadQueueRecords(qPath)
	if len(q) != 2 {
		t.Fatalf("queue should hold q1 + requeued d1, got %d", len(q))
	}
	for _, r := range q {
		if r.DeliveryAttempts != 0 || r.LastError != "" || !r.NextRetryAt.IsZero() {
			t.Errorf("record %s backoff not cleared: %+v", r.ID, r)
		}
	}
	ev, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "retry-requested" })
	if len(ev) != 1 || ev[0].Repo != "foo" {
		t.Fatalf("retry-requested event: %+v", ev)
	}
}

func TestAutoPRRetryHandler_gating(t *testing.T) {
	policyRoot := t.TempDir()
	// allowEdits=false → 403
	w := httptest.NewRecorder()
	autoPRRetryHandler(policyRoot, false, func() string { return "tok" })(w, httptest.NewRequest("POST", "/auto-pr/retry", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("allowEdits=false should 403, got %d", w.Code)
	}
	// bad CSRF → 403
	req := httptest.NewRequest("POST", "/auto-pr/retry", nil)
	req.Header.Set("X-CSRF-Token", "nope")
	w2 := httptest.NewRecorder()
	autoPRRetryHandler(policyRoot, true, func() string { return "tok" })(w2, req)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF should 403, got %d", w2.Code)
	}
	// GET → 405
	w3 := httptest.NewRecorder()
	autoPRRetryHandler(policyRoot, true, func() string { return "tok" })(w3, httptest.NewRequest("GET", "/auto-pr/retry", nil))
	if w3.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should 405, got %d", w3.Code)
	}
}

func TestDeliveryErrorHint(t *testing.T) {
	if deliveryErrorHint("") != "" {
		t.Error("empty error → empty hint")
	}
	h := deliveryErrorHint("find PR: upstream: permanent error: HTTP 403")
	if !strings.Contains(h, "Issues") || !strings.Contains(h, "repo") {
		t.Errorf("403 hint should name the Issues + repo scopes, got %q", h)
	}
}

func TestCollectAutoPR_surfacesLastError(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	qPath := filepath.Join(policyRoot, "foo", "pr-comment-queue.jsonl")
	_ = notification.AppendQueueRecord(qPath, notification.QueueRecord{ID: "q1", LastError: "HTTP 403"})
	d := collectAutoPR(policyRoot, time.Now())
	var found bool
	for _, r := range d.Repos {
		if r.Name == "foo" {
			found = true
			if r.LastError == "" || r.LastErrorHint == "" {
				t.Fatalf("expected LastError + hint surfaced, got %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("repo foo not found")
	}
}
