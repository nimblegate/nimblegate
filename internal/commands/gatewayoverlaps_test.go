// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

func TestServeGatewayOverlaps_listsRecordedOverlaps(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	registerRepoForTest(t, root, "repo-b")
	_ = gateway.AppendAudit(filepath.Join(root, "repo-a", "audit.log"), gateway.AuditRecord{
		Time: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), Repo: "repo-a", Refs: []string{"refs/heads/mine"}, Accept: true,
		Overlaps: []gateway.Overlap{{Ref: "refs/heads/mine", OtherRef: "refs/heads/other", Files: []string{"internal/doctor.go"},
			SHA: "1234567890abcdef1234567890abcdef12345678", OtherSHA: "abcdef1234567890abcdef1234567890abcdef12"}},
	})
	_ = gateway.AppendAudit(filepath.Join(root, "repo-b", "audit.log"), gateway.AuditRecord{
		Time: time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC), Repo: "repo-b", Refs: []string{"refs/heads/x"}, Accept: true,
		Overlaps: []gateway.Overlap{{Ref: "refs/heads/x", OtherRef: "refs/heads/y", Files: []string{"b-only.go"}}},
	})

	rec := httptest.NewRecorder()
	serveGatewayOverlaps(root, t.TempDir())(rec, httptest.NewRequest("GET", "/overlaps?repo=repo-a", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`id="overlaps-history"`, "internal/doctor.go", ">1234567<", ">abcdef1<", `class="gw-railitem active"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "b-only.go") {
		t.Error("repo filter should hide repo-b's overlaps")
	}
}

func TestServeGatewayOverlaps_emptyState(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	rec := httptest.NewRecorder()
	serveGatewayOverlaps(root, t.TempDir())(rec, httptest.NewRequest("GET", "/overlaps", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "No open branches share files right now.") || !strings.Contains(body, "No overlaps recorded yet.") {
		t.Errorf("empty states missing:\n%s", body)
	}
}

func TestFeedRows_overlapPillLinksToPage(t *testing.T) {
	vm := gateway.BuildView([]gateway.AuditRecord{{
		Time: time.Now(), Repo: "demo", Refs: []string{"refs/heads/mine"}, Accept: true,
		Overlaps: []gateway.Overlap{{Ref: "refs/heads/mine", OtherRef: "refs/heads/other", Files: []string{"a.txt"}}},
	}}, gateway.Filter{})
	var buf bytes.Buffer
	if err := gwTmpl.ExecuteTemplate(&buf, "rows", vm); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `href="/overlaps?repo=demo"`) || !strings.Contains(buf.String(), "mine shares 1 file(s) with other") {
		t.Errorf("overlap pill missing or wrong:\n%s", buf.String())
	}
}

func TestNotifRail_overlapEventsRoundtrip(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	view, secret, errMsg := parseNotifRailForm(map[string][]string{
		"enabled": {"1"}, "webhook_url": {"https://hooks.example.com"}, "auth_mode": {"none"}, "overlap_events": {"1"},
	})
	if errMsg != "" || !view.OverlapEvents {
		t.Fatalf("parse: errMsg=%q OverlapEvents=%v", errMsg, view.OverlapEvents)
	}
	if err := writeNotifRailTOML(root, "test", view, secret); err != nil {
		t.Fatal(err)
	}
	if !loadNotifRailView(root, "test").OverlapEvents {
		t.Error("overlap-events lost between save and load")
	}
	pol, err := gateway.FilePolicyStore{Root: root}.Load("test")
	if err != nil || pol.Notification == nil || !pol.Notification.OverlapEvents {
		t.Errorf("pre-receive would not see the setting: err=%v notification=%+v", err, pol.Notification)
	}

	var buf bytes.Buffer
	renderNotificationRailSection(&buf, "test", loadNotifRailView(root, "test"), true, "tok", "", false)
	if !strings.Contains(buf.String(), `name="overlap_events" value="1" checked`) {
		t.Error("form should render the overlap checkbox checked")
	}
}
