// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePressure_realKernelFormat(t *testing.T) {
	p, err := parsePressure([]byte(
		"some avg10=1.25 avg60=0.40 avg300=0.13 total=29\nfull avg10=0.50 avg60=0.10 avg300=0.03 total=9\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.SomeAvg10 != 1.25 || p.SomeAvg60 != 0.40 || p.SomeAvg300 != 0.13 {
		t.Errorf("some line = %+v", p)
	}
	if p.FullAvg10 != 0.50 || p.FullAvg60 != 0.10 || p.FullAvg300 != 0.03 {
		t.Errorf("full line = %+v", p)
	}
}

func TestParsePressure_unparseableIsNotZeroPressure(t *testing.T) {
	// The failure that matters: a format change must surface as "no data", not
	// as "no pressure" - the latter hides exactly what the page exists to show.
	for name, body := range map[string]string{
		"empty":       "",
		"no averages": "total=29\n",
		"wrong file":  "MemTotal: 16316360 kB\n",
	} {
		if _, err := parsePressure([]byte(body)); err == nil {
			t.Errorf("%s: want an error, got a zero-value Pressure that reads as healthy", name)
		}
	}
}

func TestPressure_StalledThresholds(t *testing.T) {
	cases := []struct {
		name string
		p    Pressure
		want bool
	}{
		{"idle", Pressure{}, false},
		{"mild some", Pressure{SomeAvg10: 4}, false},
		{"some over threshold", Pressure{SomeAvg10: 10}, true},
		{"full over threshold while some is low", Pressure{SomeAvg10: 1, FullAvg10: 5}, true},
		{"only long windows elevated", Pressure{SomeAvg300: 90}, false},
	}
	for _, c := range cases {
		if got := c.p.Stalled(); got != c.want {
			t.Errorf("%s: Stalled() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReadMemoryPressure_prefersCgroupThenFallsBack(t *testing.T) {
	dir := t.TempDir()
	cgroup := filepath.Join(dir, "memory.pressure")
	host := filepath.Join(dir, "pressure-memory")
	if err := os.WriteFile(host, []byte("some avg10=9.00 avg60=0 avg300=0 total=1\nfull avg10=0 avg60=0 avg300=0 total=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Cgroup file absent: fall back to the host file rather than reporting
	// nothing, so a bare-metal install still gets the signal.
	got := readMemoryPressureFrom([]string{cgroup, host})
	if !got.Available || got.Source != host || got.Pressure.SomeAvg10 != 9.0 {
		t.Fatalf("fallback failed: %+v", got)
	}

	// Cgroup file present: it wins, because under a container memory limit the
	// container's own pressure is the number that matters.
	if err := os.WriteFile(cgroup, []byte("some avg10=42.00 avg60=0 avg300=0 total=1\nfull avg10=0 avg60=0 avg300=0 total=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = readMemoryPressureFrom([]string{cgroup, host})
	if got.Source != cgroup || got.Pressure.SomeAvg10 != 42.0 {
		t.Errorf("cgroup should win: %+v", got)
	}

	// A garbage cgroup file must not mask a good host file.
	if err := os.WriteFile(cgroup, []byte("nonsense\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got = readMemoryPressureFrom([]string{cgroup, host}); got.Source != host {
		t.Errorf("unparseable cgroup file should fall through, got %+v", got)
	}

	// Nothing readable at all: unavailable, and explicitly not "healthy".
	if got = readMemoryPressureFrom([]string{filepath.Join(dir, "nope")}); got.Available {
		t.Errorf("want unavailable, got %+v", got)
	}
}
