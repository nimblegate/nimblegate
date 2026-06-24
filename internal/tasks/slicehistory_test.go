// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package tasks

import (
	"testing"
	"time"
)

func TestAnomalous(t *testing.T) {
	mean := 16.0 / 3.0 // mean of {2, 3, 11} ≈ 5.33
	cases := []struct {
		total int
		mean  float64
		n     int
		want  bool
	}{
		{11, mean, 3, true},  // ≥ 2×mean (10.67) and ≥3 → anomalous
		{3, mean, 3, false},  // below 2×mean
		{2, mean, 3, false},  // below
		{100, 1.0, 2, false}, // < 3 slices → never flag (not enough data)
		{1, 1.0, 3, false},   // tiny numbers: 1 < 2×1 and < 3 absolute
		{6, 3.0, 4, true},    // 6 ≥ 2×3 and ≥3
	}
	for _, c := range cases {
		if got := anomalous(c.total, c.mean, c.n); got != c.want {
			t.Errorf("anomalous(total=%d, mean=%.2f, n=%d) = %v, want %v", c.total, c.mean, c.n, got, c.want)
		}
	}
}

func TestSliceHistory_AppendRoundtripMean(t *testing.T) {
	root := t.TempDir()
	h, _ := LoadHistory(root)
	if len(h.Slices) != 0 {
		t.Fatal("fresh history should be empty")
	}
	now := time.Now().UTC()
	h.Append(CompletedSlice{Name: "a", StartedAt: now, EndedAt: now, Total: 2})
	h.Append(CompletedSlice{Name: "b", StartedAt: now, EndedAt: now, Total: 4})
	if err := h.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHistory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slices) != 2 {
		t.Fatalf("roundtrip lost slices: %+v", got)
	}
	if m := got.Mean(); m != 3.0 {
		t.Errorf("Mean = %.2f, want 3.0", m)
	}
}
