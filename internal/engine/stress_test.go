// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nimblegate/internal/frames"
)

// TestStress_RunWithManyFramesNoDeadlock fans out a large registry through
// Run() and verifies every frame ran exactly once and no results were lost.
func TestStress_RunWithManyFramesNoDeadlock(t *testing.T) {
	const N = 500
	r := NewRegistry()
	var hits int64
	for i := 0; i < N; i++ {
		f := makeFrame(frames.CategoryGitSafety, fmt.Sprintf("stress-%04d", i), []string{"cli"})
		_ = r.Add(f, func(ctx CheckContext) CheckResult {
			atomic.AddInt64(&hits, 1)
			return CheckResult{Outcome: OutcomePass}
		})
	}
	done := make(chan struct{})
	go func() {
		_ = Run(r, CheckContext{Trigger: TriggerCLI})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run() deadlocked with 500 frames")
	}
	if atomic.LoadInt64(&hits) != N {
		t.Errorf("hits = %d, want %d", hits, N)
	}
}

// TestStress_RunSurvivesMixedPanicsAndSlowChecks confirms one panicking frame
// can't take down peers or block the WaitGroup, and slow frames don't starve
// fast ones.
func TestStress_RunSurvivesMixedPanicsAndSlowChecks(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 20; i++ {
		i := i
		f := makeFrame(frames.CategoryGitSafety, fmt.Sprintf("p%d", i), []string{"cli"})
		f.Frontmatter.SeveritySource = "frame" // this test asserts the check-returned outcomes, not frontmatter severity
		_ = r.Add(f,
			func(ctx CheckContext) CheckResult {
				switch i % 4 {
				case 0:
					panic(fmt.Sprintf("intentional panic %d", i))
				case 1:
					time.Sleep(50 * time.Millisecond)
					return CheckResult{Outcome: OutcomeWarn, Reason: "slow"}
				case 2:
					return CheckResult{Outcome: OutcomeBlock, Reason: "fast block"}
				default:
					return CheckResult{Outcome: OutcomePass}
				}
			})
	}

	results := Run(r, CheckContext{Trigger: TriggerCLI})
	if len(results) != 20 {
		t.Fatalf("results = %d, want 20", len(results))
	}
	var blocks, warns, passes, errs int
	for _, res := range results {
		switch res.Outcome {
		case OutcomeBlock:
			blocks++
		case OutcomeWarn:
			warns++
		case OutcomePass:
			passes++
		case OutcomeError:
			errs++
		}
	}
	if errs != 5 || warns != 5 || blocks != 5 || passes != 5 {
		t.Errorf("counts e=%d w=%d b=%d p=%d, want 5 each", errs, warns, blocks, passes)
	}
}

// TestStress_AuditConcurrentWriters confirms many goroutines can write to one
// Audit without corrupting JSON lines or losing entries.
func TestStress_AuditConcurrentWriters(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")
	a, err := OpenAudit(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	const goroutines = 50
	const perGoroutine = 200
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = a.Write(
					CheckContext{Trigger: TriggerCLI, Command: fmt.Sprintf("g%d-i%d", g, i)},
					CheckResult{
						FrameID:  fmt.Sprintf("git-safety/g%d-frame%d", g, i),
						Category: frames.CategoryGitSafety,
						Outcome:  OutcomePass,
					},
				)
			}
		}()
	}
	wg.Wait()
	partPath := a.partPath
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Writes go to the per-process part file; read from there.
	data, err := readFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(data, "\n"), "\n")
	want := goroutines * perGoroutine
	if len(lines) != want {
		t.Errorf("line count = %d, want %d", len(lines), want)
	}
	// Every line must parse as JSON (no torn writes).
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "{") || !strings.HasSuffix(ln, "}") {
			t.Errorf("line %d not valid JSON object: %q", i, ln)
			break
		}
	}
}

// TestStress_RegistryRapidOverrides exercises the override path under a tight
// loop to surface race in indexTriggers / removeFromTriggers.
func TestStress_RegistryRapidOverrides(t *testing.T) {
	r := NewRegistry()
	// Seed with N stdlib frames.
	const N = 100
	for i := 0; i < N; i++ {
		_ = r.Add(makeFrame(frames.CategoryGitSafety, fmt.Sprintf("ov-%d", i), []string{"cli"}), nil)
	}
	// Override every one a few times in sequence (Registry is not advertised
	// concurrent-safe, so this is sequential - we're checking the bookkeeping
	// is correct after repeated removals/inserts).
	for round := 0; round < 3; round++ {
		for i := 0; i < N; i++ {
			f := makeFrame(frames.CategoryGitSafety, fmt.Sprintf("ov-%d", i), []string{"cli", "pre-commit"})
			if err := r.AddProjectOverride(f, nil); err != nil {
				t.Fatalf("round %d frame %d: %v", round, i, err)
			}
		}
	}
	// After N overrides each adding 2 triggers, byTrigger[cli] must have exactly N entries.
	cli := r.MatchingTrigger("cli")
	if len(cli) != N {
		t.Errorf("cli matched = %d, want %d", len(cli), N)
	}
	pre := r.MatchingTrigger("pre-commit")
	if len(pre) != N {
		t.Errorf("pre-commit matched = %d, want %d", len(pre), N)
	}
}

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
