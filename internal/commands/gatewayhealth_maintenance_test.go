// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway/maintenance"
)

func TestMaintenanceHealthFromStatus_neverRun(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st := maintenance.Status{
		NextSweepAt: now.Add(168 * time.Hour),
	}
	mh := maintenanceHealthFromStatus(st, now)
	if mh.LastSweepAgo != "never" {
		t.Errorf("LastSweepAgo = %q; want never", mh.LastSweepAgo)
	}
	if mh.NextSweepIn == "-" {
		t.Errorf("NextSweepIn = -; expected formatted duration")
	}
}

func TestMaintenanceHealthFromStatus_withResults(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastSweep := now.Add(-2 * time.Hour)
	st := maintenance.Status{
		LastSweepAt:   lastSweep,
		LastSweepTook: 1500 * time.Millisecond,
		NextSweepAt:   lastSweep.Add(168 * time.Hour),
		SweepCount:    7,
		PerRepo: []maintenance.RepoResult{
			{Repo: "a.git", StartedAt: lastSweep, Took: 200 * time.Millisecond},
			{Repo: "b.git", StartedAt: lastSweep, Took: 350 * time.Millisecond, Err: errors.New("simulated fail")},
		},
	}

	mh := maintenanceHealthFromStatus(st, now)
	if mh.RepoCount != 2 {
		t.Errorf("RepoCount = %d; want 2", mh.RepoCount)
	}
	if mh.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d; want 1", mh.ErrorCount)
	}
	if mh.SweepCount != 7 {
		t.Errorf("SweepCount = %d; want 7", mh.SweepCount)
	}
	if mh.LastSweepAgo != "2h ago" {
		t.Errorf("LastSweepAgo = %q; want 2h ago", mh.LastSweepAgo)
	}
	if len(mh.PerRepo) != 2 {
		t.Fatalf("PerRepo len = %d; want 2", len(mh.PerRepo))
	}
	if mh.PerRepo[1].Err != "simulated fail" {
		t.Errorf("expected b.git Err passed through; got %q", mh.PerRepo[1].Err)
	}
}

func TestRenderHealth_includesMaintenanceWhenSet(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	data := healthData{
		PID:    1234,
		Uptime: "1h",
		Maintenance: &maintenanceHealth{
			Interval:     "168h",
			LastSweepAgo: "2h ago",
			NextSweepIn:  "166h",
			SweepCount:   1,
			RepoCount:    2,
			ErrorCount:   0,
			PerRepo: []maintenanceRepoHealth{
				{Repo: "a.git", Ago: "2h ago", Took: "200ms"},
				{Repo: "b.git", Ago: "2h ago", Took: "350ms"},
			},
		},
	}
	_ = now
	var body bytes.Buffer
	if err := renderHealth(&body, data); err != nil {
		t.Fatalf("renderHealth: %v", err)
	}
	out := body.String()
	for _, want := range []string{
		"Maintenance",
		"gc every 168h",
		"last sweep 2h ago",
		"per-repo",
		"a.git",
		"b.git",
		"200ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered /health missing %q; got: %s", want, out)
		}
	}
}

func TestRenderHealth_omitsMaintenanceWhenNil(t *testing.T) {
	data := healthData{
		PID:            1234,
		Uptime:         "1h",
		DiskFreeStatus: "ok",
		DiskFreeBytes:  "10G",
	}
	var body bytes.Buffer
	if err := renderHealth(&body, data); err != nil {
		t.Fatal(err)
	}
	out := body.String()
	if strings.Contains(out, "Maintenance") {
		t.Error("Maintenance section should be hidden when nil")
	}
}

func TestFormatAgo_acrossRanges(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, c := range cases {
		if got := formatAgo(c.d); got != c.want {
			t.Errorf("formatAgo(%s) = %q; want %q", c.d, got, c.want)
		}
	}
}
