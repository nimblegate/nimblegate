// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
	"time"
)

func rec(t time.Time, repo string, accept bool, msgs ...string) AuditRecord {
	return AuditRecord{Time: t, Repo: repo, Refs: []string{"refs/heads/main"}, Accept: accept, Messages: msgs}
}

func TestBuildRefDisplays_withSHAs(t *testing.T) {
	updates := []RefUpdate{
		{Name: "refs/heads/main", OldRev: "0000000000000000000000000000000000000000", NewRev: "6849c3586e1234567890abcdef1234567890abcd"},
		{Name: "refs/tags/v0.1.1", OldRev: "0000000000000000000000000000000000000000", NewRev: "abc123def4567890abcdef1234567890abcdef12"},
	}
	got := buildRefDisplays([]string{"refs/heads/main", "refs/tags/v0.1.1"}, updates)
	if len(got) != 2 {
		t.Fatalf("got %d displays, want 2", len(got))
	}
	if got[0].Name != "refs/heads/main" || got[0].ShortSHA != "6849c35" {
		t.Errorf("display[0] = %+v, want name=refs/heads/main sha=6849c35", got[0])
	}
	if got[1].ShortSHA != "abc123d" {
		t.Errorf("display[1] ShortSHA = %q, want abc123d", got[1].ShortSHA)
	}
}

func TestBuildRefDisplays_legacyAuditNoSHAs(t *testing.T) {
	// Old audit lines stored only Refs (names). Helper should fall back to
	// names-only displays so the feed still renders.
	got := buildRefDisplays([]string{"refs/heads/main"}, nil)
	if len(got) != 1 || got[0].Name != "refs/heads/main" || got[0].ShortSHA != "" {
		t.Errorf("legacy fallback wrong: %+v", got)
	}
}

func TestBuildRefDisplays_deleteHasNoSHA(t *testing.T) {
	updates := []RefUpdate{
		{Name: "refs/heads/feat-old", OldRev: "abc123def4567890abcdef1234567890abcdef12", NewRev: "0000000000000000000000000000000000000000"},
	}
	got := buildRefDisplays([]string{"refs/heads/feat-old"}, updates)
	if len(got) != 1 || got[0].ShortSHA != "" {
		t.Errorf("delete should have empty ShortSHA: %+v", got)
	}
}

func TestBuildView(t *testing.T) {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	recs := []AuditRecord{
		rec(base.Add(1*time.Minute), "api", true),
		rec(base.Add(2*time.Minute), "web", false, "refs/heads/main: BLOCK [security/no-private-keys-in-repo] key found"),
		rec(base.Add(3*time.Minute), "api", false, "refs/heads/main: BLOCK [security/no-private-keys-in-repo] key found"),
		rec(base.Add(4*time.Minute), "web", true),
	}
	vm := BuildView(recs, Filter{})
	if vm.Summary.Repos != 2 || vm.Summary.Accepts != 2 || vm.Summary.Rejects != 2 {
		t.Errorf("summary counts wrong: %+v", vm.Summary)
	}
	if vm.Summary.TopBlock != "security/no-private-keys-in-repo" || vm.Summary.TopBlockN != 2 {
		t.Errorf("top block wrong: %q x%d", vm.Summary.TopBlock, vm.Summary.TopBlockN)
	}
	if len(vm.Repos) != 2 || vm.Repos[0] != "api" || vm.Repos[1] != "web" {
		t.Errorf("repo list wrong/unsorted: %v", vm.Repos)
	}
	if len(vm.Rows) != 4 || vm.Rows[0].Repo != "web" || !vm.Rows[0].Accept {
		t.Errorf("rows not newest-first: %+v", vm.Rows)
	}
}

func TestBuildView_carriesFindings(t *testing.T) {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	finds := []Finding{{ID: "app-correctness/no-owner-todos", Severity: "WARN", Message: "TODO with no owner"}}
	recs := []AuditRecord{
		{Time: base, Repo: "api", Refs: []string{"refs/heads/main"}, Accept: true, Findings: finds},
	}
	vm := BuildView(recs, Filter{})
	if len(vm.Rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(vm.Rows))
	}
	if len(vm.Rows[0].Findings) != 1 {
		t.Fatalf("row should carry findings, got %+v", vm.Rows[0].Findings)
	}
	if got := vm.Rows[0].Findings[0]; got.ID != finds[0].ID || got.Severity != "WARN" {
		t.Errorf("finding not carried verbatim: %+v", got)
	}
}

func TestBuildView_filters(t *testing.T) {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	recs := []AuditRecord{
		rec(base.Add(1*time.Minute), "api", true),
		rec(base.Add(2*time.Minute), "api", false, "x: BLOCK [convention/y] z"),
		rec(base.Add(3*time.Minute), "web", true),
	}
	if vm := BuildView(recs, Filter{Repo: "api"}); len(vm.Rows) != 2 {
		t.Errorf("repo filter = %d rows, want 2", len(vm.Rows))
	}
	if vm := BuildView(recs, Filter{RejectsOnly: true}); len(vm.Rows) != 1 || vm.Rows[0].Accept {
		t.Errorf("rejects-only = %+v, want 1 reject", vm.Rows)
	}
	if vm := BuildView(recs, Filter{Limit: 1}); len(vm.Rows) != 1 {
		t.Errorf("limit cap = %d rows, want 1", len(vm.Rows))
	}
	if vm := BuildView(recs, Filter{Repo: "api"}); vm.Summary.Repos != 2 {
		t.Errorf("summary should count all repos regardless of filter, got %d", vm.Summary.Repos)
	}
}

func TestFrameFromMessage(t *testing.T) {
	if got := frameFromMessage("refs/heads/main: BLOCK [security/no-private-keys-in-repo] reason"); got != "security/no-private-keys-in-repo" {
		t.Errorf("frameFromMessage = %q", got)
	}
	if got := frameFromMessage("no brackets here"); got != "" {
		t.Errorf("no-bracket should be empty, got %q", got)
	}
	if got := frameFromMessage("refs/heads/main: BLOCK [security/no-close"); got != "" {
		t.Errorf("open-bracket-without-close should be empty, got %q", got)
	}
}

func TestLocationsFromMessages_extractsPathLineFromGatewayMessage(t *testing.T) {
	msgs := []string{
		"refs/heads/main: BLOCK [command-safety/curl-pipe-shell] pipe-to-shell patterns detected: internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/install-via-pipe.sh:4 - curl|wget piped to shell; internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/process-substitution.sh:3 - shell with process-substituted curl/wget; internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/wget-pipe.sh:3 - curl|wget piped to shell",
	}
	want := []string{
		"internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/install-via-pipe.sh:4",
		"internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/process-substitution.sh:3",
		"internal/stdlib/testdata/command-safety/curl-pipe-shell/positives/wget-pipe.sh:3",
	}
	got := LocationsFromMessages(msgs)
	if len(got) != len(want) {
		t.Fatalf("len: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestLocationsFromMessages_dedupesAcrossMessages(t *testing.T) {
	msgs := []string{
		"msg one: same/file.go:1 first time",
		"msg two: same/file.go:1 again",
		"msg three: other/file.go:2",
	}
	got := LocationsFromMessages(msgs)
	want := []string{"same/file.go:1", "other/file.go:2"}
	if len(got) != len(want) {
		t.Fatalf("len: got %v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d]: got %q want %q", i, got[i], w)
		}
	}
}

func TestLocationsFromMessages_skipsFrameIDsWithoutLine(t *testing.T) {
	msgs := []string{
		"BLOCK [security/no-private-keys-in-repo] some message",
		"WARN [command-safety/curl-pipe-shell] no path here",
	}
	got := LocationsFromMessages(msgs)
	if len(got) != 0 {
		t.Errorf("frame IDs should not match (no extension, no line): %v", got)
	}
}

func TestLocationsFromMessages_extractsPemAndOtherExtensions(t *testing.T) {
	msgs := []string{
		"BLOCK [security/no-private-keys-in-repo] private keys detected (content redacted): internal/stdlib/testdata/security/no-private-keys-in-repo/positives/ec-key.pem:1 - PEM EC private key",
		"WARN [convention/html-required-meta] HTML pages missing required meta: tools/_template/partials/footer.html:0 - missing required meta",
	}
	got := LocationsFromMessages(msgs)
	want := []string{
		"internal/stdlib/testdata/security/no-private-keys-in-repo/positives/ec-key.pem:1",
		"tools/_template/partials/footer.html:0",
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d]: got %q want %q", i, got[i], w)
		}
	}
}

func TestLocationsFromMessages_skipsVersionStrings(t *testing.T) {
	msgs := []string{"upgrading from 1.2.3:4 to 5.6.7:8 no actual path here"}
	got := LocationsFromMessages(msgs)
	if len(got) != 0 {
		t.Errorf("version-string-like fragments must not match - ext must start with a letter: %v", got)
	}
}

func TestLocationsFromMessages_emptyInputReturnsNil(t *testing.T) {
	if got := LocationsFromMessages(nil); got != nil {
		t.Errorf("nil input: got %v want nil", got)
	}
	if got := LocationsFromMessages([]string{}); got != nil {
		t.Errorf("empty slice: got %v want nil", got)
	}
	if got := LocationsFromMessages([]string{""}); got != nil {
		t.Errorf("slice with empty string: got %v want nil", got)
	}
}

func TestNotifStatusView_buckets(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		status    *NotificationStatus
		wantNil   bool
		wantSym   string
		wantInd   string
		wantMsgIn string
	}{
		{"nil yields nil", nil, true, "", "", ""},
		{
			"deadlettered → ⚠",
			&NotificationStatus{Deadlettered: true, DeliveryAttempts: 12},
			false, "warn", "deadlettered", "12 attempts",
		},
		{
			"inline succeeded → 📨",
			&NotificationStatus{InlineSucceeded: true, InlineAttempted: true},
			false, "notif", "delivered", "PR comment delivered",
		},
		{
			"daemon-delivered → 📨",
			&NotificationStatus{DeliveredAt: now, DeliveryAttempts: 2},
			false, "notif", "delivered", "PR comment delivered",
		},
		{
			"queued → 🕐",
			&NotificationStatus{EventID: "e1", QueuedAt: now},
			false, "pending", "queued", "queue",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := notifStatusView(c.status)
			if c.wantNil {
				if got != nil {
					t.Errorf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want non-nil")
			}
			if got.Symbol != c.wantSym {
				t.Errorf("symbol = %q, want %q", got.Symbol, c.wantSym)
			}
			if got.Indicator != c.wantInd {
				t.Errorf("indicator = %q, want %q", got.Indicator, c.wantInd)
			}
			if c.wantMsgIn != "" && !contains(got.Message, c.wantMsgIn) {
				t.Errorf("message %q does not contain %q", got.Message, c.wantMsgIn)
			}
		})
	}
}

func TestBuildView_carriesNotificationStatus(t *testing.T) {
	base := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	recs := []AuditRecord{{
		Time:   base,
		Repo:   "api",
		Refs:   []string{"refs/heads/main"},
		Accept: false,
		Notification: &NotificationStatus{
			EventID:          "evt-1",
			QueuedAt:         base,
			Deadlettered:     true,
			DeliveryAttempts: 20,
		},
	}}
	vm := BuildView(recs, Filter{})
	if len(vm.Rows) != 1 {
		t.Fatalf("rows = %d", len(vm.Rows))
	}
	if vm.Rows[0].NotificationStatus == nil {
		t.Fatalf("NotificationStatus not populated")
	}
	if vm.Rows[0].NotificationStatus.Indicator != "deadlettered" {
		t.Errorf("got indicator %q", vm.Rows[0].NotificationStatus.Indicator)
	}
}

func TestBuildView_omitsNotificationStatusOnLegacyRow(t *testing.T) {
	base := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	// AuditRecord without Notification (older log line).
	recs := []AuditRecord{rec(base, "api", true)}
	vm := BuildView(recs, Filter{})
	if vm.Rows[0].NotificationStatus != nil {
		t.Errorf("legacy row should not get a NotificationStatus")
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func TestBuildView_BeforeFilterAndPagingFields(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	recs := []AuditRecord{
		{Time: base, Repo: "r", Accept: true},
		{Time: base.Add(time.Hour), Repo: "r", Accept: true},
		{Time: base.Add(2 * time.Hour), Repo: "r", Accept: true},
	}
	vm := BuildView(recs, Filter{Before: base.Add(2 * time.Hour), Limit: 500})
	if len(vm.Rows) != 2 {
		t.Fatalf("before-filter: want 2 rows, got %d", len(vm.Rows))
	}
	// rows are newest-first; oldest rendered is base+0h
	if !vm.Summary.OldestTime.Equal(base) {
		t.Fatalf("OldestTime: want %s, got %s", base, vm.Summary.OldestTime)
	}
	if vm.Summary.HasMore {
		t.Fatal("HasMore should be false when page not truncated")
	}
}

func TestBuildView_HasMoreWhenTruncated(t *testing.T) {
	var recs []AuditRecord
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		recs = append(recs, AuditRecord{Time: base.Add(time.Duration(i) * time.Hour), Repo: "r", Accept: true})
	}
	vm := BuildView(recs, Filter{Limit: 3})
	if len(vm.Rows) != 3 || !vm.Summary.HasMore {
		t.Fatalf("want 3 rows + HasMore, got rows=%d hasMore=%v", len(vm.Rows), vm.Summary.HasMore)
	}
}
