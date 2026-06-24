// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"path/filepath"
	"testing"

	"nimblegate/internal/frames"
)

// BenchmarkRun_100Frames measures the cost of parallel Run over a typical
// registry size with always-pass check funcs.
func BenchmarkRun_100Frames(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		_ = r.Add(makeFrame(frames.CategoryGitSafety, fmt.Sprintf("b-%d", i), []string{"cli"}),
			func(ctx CheckContext) CheckResult { return CheckResult{Outcome: OutcomePass} })
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Run(r, CheckContext{Trigger: TriggerCLI})
	}
}

// BenchmarkRun_500Frames - does cost grow linearly?
func BenchmarkRun_500Frames(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 500; i++ {
		_ = r.Add(makeFrame(frames.CategoryGitSafety, fmt.Sprintf("b-%d", i), []string{"cli"}),
			func(ctx CheckContext) CheckResult { return CheckResult{Outcome: OutcomePass} })
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Run(r, CheckContext{Trigger: TriggerCLI})
	}
}

// BenchmarkAudit_Write - sequential audit writes (lock contention isn't
// captured here; see BenchmarkAudit_ParallelWrite for that).
func BenchmarkAudit_Write(b *testing.B) {
	tmp := b.TempDir()
	a, err := OpenAudit(filepath.Join(tmp, "audit.log"))
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	ctx := CheckContext{Trigger: TriggerCLI, Command: "bench"}
	res := CheckResult{FrameID: "git-safety/bench", Category: frames.CategoryGitSafety, Outcome: OutcomePass}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.Write(ctx, res)
	}
}

// BenchmarkAudit_ParallelWrite - mutex contention under load.
func BenchmarkAudit_ParallelWrite(b *testing.B) {
	tmp := b.TempDir()
	a, err := OpenAudit(filepath.Join(tmp, "audit.log"))
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := CheckContext{Trigger: TriggerCLI, Command: "bench"}
		res := CheckResult{FrameID: "git-safety/bench", Category: frames.CategoryGitSafety, Outcome: OutcomePass}
		for pb.Next() {
			_ = a.Write(ctx, res)
		}
	})
}

// BenchmarkRegistry_Add - measures cost of building a registry.
func BenchmarkRegistry_Add(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		for j := 0; j < 50; j++ {
			_ = r.Add(makeFrame(frames.CategoryGitSafety, fmt.Sprintf("b-%d", j), []string{"cli"}), nil)
		}
	}
}
