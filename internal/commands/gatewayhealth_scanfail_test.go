// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

// A push the gate could not scan is invisible to the pusher by design, and in
// observe mode it is relayed as if nothing happened. /health is where the
// operator finds out scanning has stopped, so it has to actually show up.

func writeScanFailEvents(t *testing.T, root string, evs []gateway.Event) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range evs {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "_events.jsonl"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectHealth_countsRecentScanFailures(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeScanFailEvents(t, root, []gateway.Event{
		{Timestamp: now.Add(-30 * time.Hour), Event: "scan-failed", Repo: "demo", Payload: map[string]any{"detail": "ancient"}},
		{Timestamp: now.Add(-2 * time.Hour), Event: "scan-failed", Repo: "demo", Payload: map[string]any{"detail": "first today"}},
		{Timestamp: now.Add(-1 * time.Hour), Event: "relay-ok", Repo: "demo", OK: true},
		{Timestamp: now.Add(-10 * time.Minute), Event: "scan-failed", Repo: "demo", Payload: map[string]any{"detail": "no space left on device"}},
	})

	d := collectHealth(root, "", now.Add(-time.Hour), now)
	if d.ScanFailures24h != 2 {
		t.Errorf("ScanFailures24h = %d, want 2 (the 30h-old one is outside the window)", d.ScanFailures24h)
	}
	if !strings.Contains(d.ScanFailureLast, "no space left") {
		t.Errorf("should surface the most recent detail, got %q", d.ScanFailureLast)
	}
}

func TestCollectHealth_noScanFailuresIsSilent(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeScanFailEvents(t, root, []gateway.Event{
		{Timestamp: now.Add(-time.Hour), Event: "relay-ok", Repo: "demo", OK: true},
	})

	d := collectHealth(root, "", now.Add(-time.Hour), now)
	if d.ScanFailures24h != 0 || d.ScanFailureLast != "" {
		t.Errorf("a healthy gateway must not render the line, got %d %q", d.ScanFailures24h, d.ScanFailureLast)
	}
	var body bytes.Buffer
	if err := renderHealth(&body, d); err != nil {
		t.Fatalf("renderHealth: %v", err)
	}
	if strings.Contains(body.String(), "could not be scanned") {
		t.Error("scan-failure line must be absent when there are none")
	}
}

func TestRenderHealth_showsScanFailureWarning(t *testing.T) {
	var body bytes.Buffer
	err := renderHealth(&body, healthData{
		PID: 1, Uptime: "1h",
		ScanFailures24h: 3,
		ScanFailureLast: "refs/heads/main: ERROR [gateway/scan-failed] materialize: No space left on device",
	})
	if err != nil {
		t.Fatalf("renderHealth: %v", err)
	}
	out := body.String()
	for _, want := range []string{"Gate scans", "3 push(es) could not be scanned", "No space left on device"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered /health missing %q; got: %s", want, out)
		}
	}
}

func TestRenderHealth_memoryPressureStates(t *testing.T) {
	for _, c := range []struct {
		name       string
		data       healthData
		want, deny string
	}{
		{
			name: "warn",
			data: healthData{PID: 1, Uptime: "1h", MemPressureStatus: "warn",
				MemPressureDetail: "some 22.0% / full 7.0% (last 10s) - tasks are stalling on memory"},
			want: "some 22.0%",
		},
		{
			name: "ok",
			data: healthData{PID: 1, Uptime: "1h", MemPressureStatus: "ok",
				MemPressureDetail: "some 0.0% / full 0.0% (last 10s)"},
			want: "Memory pressure",
		},
		{
			name: "unavailable reads as no data, not as fine",
			data: healthData{PID: 1, Uptime: "1h", MemPressureStatus: "-",
				MemPressureDetail: "unavailable (kernel without PSI)"},
			want: "unavailable",
			deny: "gw-health-status--",
		},
	} {
		var body bytes.Buffer
		if err := renderHealth(&body, c.data); err != nil {
			t.Fatalf("%s: renderHealth: %v", c.name, err)
		}
		out := body.String()
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: missing %q in:\n%s", c.name, c.want, out)
		}
		if c.deny != "" && strings.Contains(out, c.deny) {
			t.Errorf("%s: should not render %q", c.name, c.deny)
		}
	}
}

func TestCollectHealth_readsMemoryPressureFromTheKernel(t *testing.T) {
	// Whatever this machine reports, the page must end up in one of the three
	// defined states - never blank, which would silently drop the signal.
	d := collectHealth(t.TempDir(), "", time.Now().Add(-time.Minute), time.Now())
	switch d.MemPressureStatus {
	case "ok", "warn", "-":
	default:
		t.Errorf("MemPressureStatus = %q; want ok, warn or -", d.MemPressureStatus)
	}
	if d.MemPressureDetail == "" {
		t.Error("detail must always say something, even when unavailable")
	}
}

func TestCollectStagingHealth_reportsTheEffectiveDir(t *testing.T) {
	policyRoot := t.TempDir()
	reposRoot := t.TempDir()
	now := time.Now()

	// Default location, not yet created: reported, with the parent's free space.
	var d healthData
	collectStagingHealth(&d, policyRoot, reposRoot, now)
	if d.StagingDir != filepath.Join(reposRoot, "_scan-tmp") {
		t.Errorf("StagingDir = %q", d.StagingDir)
	}
	if d.StagingStatus != "ok" || !strings.Contains(d.StagingDetail, "created on first push") {
		t.Errorf("status=%q detail=%q", d.StagingStatus, d.StagingDetail)
	}

	// A configured dir is named as such, so an operator can confirm it took.
	custom := filepath.Join(t.TempDir(), "fast")
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "gateway.toml"),
		[]byte("[gateway]\nscan_tmpdir = \""+custom+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d = healthData{}
	collectStagingHealth(&d, policyRoot, reposRoot, now)
	if d.StagingDir != custom || !strings.Contains(d.StagingDetail, "scan_tmpdir") {
		t.Errorf("configured dir not surfaced: %q / %q", d.StagingDir, d.StagingDetail)
	}
	if strings.Contains(d.StagingDetail, "created on first push") {
		t.Error("an existing dir should not claim it will be created")
	}
}

func TestCollectStagingHealth_warnsWhenTheHookFellBack(t *testing.T) {
	// The failure this line exists for: the configured dir could not be created,
	// staging silently reverted to $TMPDIR, and nothing else would say so.
	policyRoot := t.TempDir()
	reposRoot := t.TempDir()
	now := time.Now()
	writeScanFailEvents(t, policyRoot, []gateway.Event{
		{Timestamp: now.Add(-30 * time.Hour), Event: "scan-staging-fallback", Repo: "demo", Payload: map[string]any{"configured": "/old"}},
	})
	var d healthData
	collectStagingHealth(&d, policyRoot, reposRoot, now)
	if d.StagingStatus == "warn" {
		t.Error("a fallback from 30h ago is stale; the operator may have fixed it")
	}

	writeScanFailEvents(t, policyRoot, []gateway.Event{
		{Timestamp: now.Add(-time.Hour), Event: "scan-staging-fallback", Repo: "demo", Payload: map[string]any{"configured": "/mnt/typo"}},
	})
	d = healthData{}
	collectStagingHealth(&d, policyRoot, reposRoot, now)
	if d.StagingStatus != "warn" || !strings.Contains(d.StagingDetail, "$TMPDIR") {
		t.Errorf("recent fallback must warn, got %q / %q", d.StagingStatus, d.StagingDetail)
	}
}

func TestCollectStagingHealth_silentWithoutReposRoot(t *testing.T) {
	var d healthData
	collectStagingHealth(&d, t.TempDir(), "", time.Now())
	if d.StagingStatus != "" {
		t.Errorf("no repos root means the gate is not serving pushes here; got %q", d.StagingStatus)
	}
}
