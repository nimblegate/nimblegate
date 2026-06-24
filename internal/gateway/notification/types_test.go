// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNotification_JSONRoundtrip(t *testing.T) {
	n := Notification{
		SchemaVersion: "1.0",
		Event:         "push.rejected",
		EventID:       "evt_2026-06-04T18-23-45Z_a1b2c3d4",
		Gateway: GatewayInfo{
			Name:       "nimblegate",
			Version:    "v0.1.0",
			InstanceID: "gw-host.lan",
		},
		Repo: RepoInfo{Name: "nimblegate", UpstreamURL: "https://github.com/nimblegate/nimblegate"},
		Push: PushInfo{
			Timestamp:            time.Date(2026, 6, 4, 18, 23, 45, 0, time.UTC),
			PusherKeyFingerprint: "SHA256:abc",
			Refs:                 []RefInfo{{Name: "refs/heads/main", OldSHA: "ca3f056", NewSHA: "5ea730c", Type: "update"}},
		},
		Decision: DecisionInfo{
			Accepted: false,
			Findings: []Finding{{FrameID: "security/no-private-keys-in-repo", Severity: "BLOCK", Message: "PEM EC private key found", File: "config/key.pem", Line: 1, Hint: "Move to env vars; rotate the key.", Fingerprint: "sha256:e7f8a9"}},
		},
	}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Notification
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != "1.0" || got.Event != "push.rejected" {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
	if len(got.Decision.Findings) != 1 || got.Decision.Findings[0].Fingerprint != "sha256:e7f8a9" {
		t.Errorf("finding roundtrip wrong: %+v", got.Decision.Findings)
	}
}

func TestBuild_Reject_PopulatesEventAndIDs(t *testing.T) {
	in := BuildInput{
		Repo:        "demo",
		UpstreamURL: "https://upstream.test/demo",
		Refs: []BuildRef{{
			Name:   "refs/heads/main",
			OldSHA: "abc123",
			NewSHA: "def456",
		}},
		Findings: []BuildFinding{{
			FrameID:  "security/no-private-keys-in-repo",
			Severity: "BLOCK",
			Message:  "config/key.pem:1 - PEM EC private key found",
		}},
		Suppressed: []BuildSuppression{{
			FrameID: "security/x",
			File:    "internal/x_test.go",
			Label:   "PEM key",
		}},
	}
	n := Build(in, "v0.1.0", "gw-host.lan")
	if n.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", n.SchemaVersion, SchemaVersion)
	}
	if n.Event != "push.rejected" {
		t.Errorf("Event = %q, want push.rejected", n.Event)
	}
	if n.EventID == "" || !strings.HasPrefix(n.EventID, "evt_") {
		t.Errorf("EventID = %q, want evt_-prefixed", n.EventID)
	}
	if n.Gateway.Version != "v0.1.0" || n.Gateway.InstanceID != "gw-host.lan" {
		t.Errorf("gateway info wrong: %+v", n.Gateway)
	}
	if n.Repo.Name != "demo" || n.Repo.UpstreamURL != "https://upstream.test/demo" {
		t.Errorf("repo info wrong: %+v", n.Repo)
	}
	if len(n.Push.Refs) != 1 || n.Push.Refs[0].Type != "update" {
		t.Errorf("Push.Refs wrong: %+v", n.Push.Refs)
	}
	if len(n.Decision.Findings) != 1 {
		t.Fatalf("Findings = %+v", n.Decision.Findings)
	}
	f := n.Decision.Findings[0]
	if f.File != "config/key.pem" || f.Line != 1 {
		t.Errorf("file/line parse wrong: file=%q line=%d", f.File, f.Line)
	}
	if f.Fingerprint == "" || !strings.HasPrefix(f.Fingerprint, "sha256:") {
		t.Errorf("Fingerprint missing: %q", f.Fingerprint)
	}
	if len(n.Decision.Suppressed) != 1 || n.Decision.Suppressed[0].Reason != "PEM key" {
		t.Errorf("Suppressed wrong: %+v", n.Decision.Suppressed)
	}
}

func TestBuild_Observed_SetsObservedEvent(t *testing.T) {
	in := BuildInput{
		Repo:        "demo",
		UpstreamURL: "https://upstream.test/demo",
		Observed:    true,
		Refs:        []BuildRef{{Name: "refs/heads/main", OldSHA: "a", NewSHA: "b"}},
	}
	n := Build(in, "v0.1.0", "gw")
	if n.Event != "push.observed" {
		t.Errorf("Event = %q, want push.observed", n.Event)
	}
	if !n.Decision.Observed {
		t.Error("DecisionInfo.Observed should be true")
	}
}

func TestBuild_CreateAndDeleteRefTypes(t *testing.T) {
	in := BuildInput{
		Refs: []BuildRef{
			{Name: "refs/heads/new", OldSHA: zeroRev, NewSHA: "abc"},
			{Name: "refs/heads/gone", OldSHA: "abc", NewSHA: zeroRev},
		},
	}
	n := Build(in, "v", "i")
	if n.Push.Refs[0].Type != "create" {
		t.Errorf("[0].Type = %q, want create", n.Push.Refs[0].Type)
	}
	if n.Push.Refs[1].Type != "delete" {
		t.Errorf("[1].Type = %q, want delete", n.Push.Refs[1].Type)
	}
}

func TestParseFileLine_FormatsCovered(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
	}{
		{"config/key.pem:12 - PEM key", "config/key.pem", 12},
		{"path/to/file.go:1", "path/to/file.go", 1},
		// Mid-string file:line (summary prefix) - returned empty when anchored.
		{"pipe-to-shell patterns detected: deploy.sh:1 - curl|wget piped to shell", "deploy.sh", 1},
		{"no-line-info reason here", "", 0},
		{"", "", 0},
	}
	for _, c := range cases {
		f, l := parseFileLine(c.in)
		if f != c.wantFile || l != c.wantLine {
			t.Errorf("parseFileLine(%q) = (%q, %d), want (%q, %d)", c.in, f, l, c.wantFile, c.wantLine)
		}
	}
}

func TestNewEventID_UniqueAndPrefixed(t *testing.T) {
	n1 := newEventID(time.Now())
	n2 := newEventID(time.Now())
	if n1 == n2 {
		t.Errorf("IDs should differ: %q == %q", n1, n2)
	}
	if !strings.HasPrefix(n1, "evt_") {
		t.Errorf("ID prefix wrong: %q", n1)
	}
}
