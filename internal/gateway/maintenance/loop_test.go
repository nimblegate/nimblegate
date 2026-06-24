// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock fires ticker channels on demand, lets tests drive the loop
// deterministically. Now() advances only when Tick() advances it.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTicker{ch: make(chan time.Time, 1)}
	c.tickers = append(c.tickers, t)
	return t
}

// Advance moves Now() forward and fires every active ticker once.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, t := range tickers {
		select {
		case t.ch <- now:
		default:
		}
	}
}

type fakeTicker struct {
	ch chan time.Time
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()               { close(t.ch) }

// recordingEvents collects every Append call for assertion.
type recordingEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Name    string
	Payload map[string]any
}

func (r *recordingEvents) Append(name string, payload map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make(map[string]any, len(payload))
	for k, v := range payload {
		cp[k] = v
	}
	r.events = append(r.events, recordedEvent{Name: name, Payload: cp})
}

func (r *recordingEvents) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Name
	}
	return out
}

// stubGC returns canned results without actually shelling out.
type stubGC struct {
	mu       sync.Mutex
	calls    []string
	failures map[string]error
	took     time.Duration
}

func (s *stubGC) Run(_ context.Context, repoGitDir string) RepoResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, repoGitDir)
	return RepoResult{
		Repo:      filepath.Base(repoGitDir),
		Took:      s.took,
		Err:       s.failures[filepath.Base(repoGitDir)],
		StartedAt: time.Now(),
	}
}

// makeFakeRepoTree lays down empty *.git dirs (which Findings simulate by
// presence - gc.Run is stubbed so the dirs don't have to be real git repos).
func makeFakeRepoTree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunner_disabledExitsImmediately(t *testing.T) {
	root := makeFakeRepoTree(t, "x.git")
	gc := &stubGC{}
	events := &recordingEvents{}
	clock := newFakeClock()
	r := NewRunner(Config{Enabled: false}, root, gc, events, clock)

	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled Runner should return immediately")
	}
	if len(gc.calls) != 0 {
		t.Errorf("disabled Runner called gc %v times; want 0", len(gc.calls))
	}
}

func TestRunner_firstSweepAfterInterval(t *testing.T) {
	root := makeFakeRepoTree(t, "a.git", "b.git")
	gc := &stubGC{}
	events := &recordingEvents{}
	clock := newFakeClock()
	cfg := Config{Enabled: true, Interval: 168 * time.Hour}
	r := NewRunner(cfg, root, gc, events, clock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	// Give the goroutine a moment to register its ticker
	time.Sleep(20 * time.Millisecond)

	// One interval passes → one sweep
	clock.Advance(168 * time.Hour)
	for i := 0; i < 50; i++ {
		gc.mu.Lock()
		n := len(gc.calls)
		gc.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	gc.mu.Lock()
	calls := append([]string(nil), gc.calls...)
	gc.mu.Unlock()
	sort.Strings(calls)
	if len(calls) != 2 {
		t.Fatalf("after one tick: gc.calls = %v; want 2 (both repos)", calls)
	}
	if filepath.Base(calls[0]) != "a.git" || filepath.Base(calls[1]) != "b.git" {
		t.Errorf("expected a.git + b.git sweep; got %v", calls)
	}

	cancel()
	<-done
}

func TestRunner_perRepoErrorDoesntAbortSweep(t *testing.T) {
	root := makeFakeRepoTree(t, "ok1.git", "bad.git", "ok2.git")
	gc := &stubGC{failures: map[string]error{"bad.git": errors.New("simulated gc fail")}}
	events := &recordingEvents{}
	clock := newFakeClock()
	r := NewRunner(Config{Enabled: true, Interval: time.Hour}, root, gc, events, clock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)

	clock.Advance(time.Hour)
	for i := 0; i < 50; i++ {
		gc.mu.Lock()
		n := len(gc.calls)
		gc.mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(gc.calls) != 3 {
		t.Fatalf("after one tick: gc.calls = %v; want 3 (all three repos despite middle failure)", gc.calls)
	}

	// Status should reflect the error on bad.git.
	st := r.Status()
	if st.SweepCount != 1 {
		t.Errorf("SweepCount = %d; want 1", st.SweepCount)
	}
	if len(st.PerRepo) != 3 {
		t.Errorf("PerRepo count = %d; want 3", len(st.PerRepo))
	}
	var sawBadErr bool
	for _, rr := range st.PerRepo {
		if rr.Repo == "bad.git" && rr.Err != nil {
			sawBadErr = true
		}
	}
	if !sawBadErr {
		t.Error("bad.git should have non-nil Err in PerRepo")
	}

	// Events should include one maintenance-gc-failed for bad.git.
	gotFailed := false
	for _, e := range events.events {
		if e.Name == "maintenance-gc-failed" {
			gotFailed = true
		}
	}
	if !gotFailed {
		t.Errorf("expected maintenance-gc-failed event; got names=%v", events.names())
	}

	cancel()
	<-done
}

func TestRunner_findBareReposSkipsNonGitAndUnderscore(t *testing.T) {
	root := makeFakeRepoTree(t, "real.git", "_repos", "notgit", "another.git")
	r := &Runner{reposRoot: root}
	got := r.findBareRepos()
	if len(got) != 2 {
		t.Fatalf("got %d repos; want 2 (real.git, another.git); list=%v", len(got), got)
	}
	gotNames := []string{filepath.Base(got[0]), filepath.Base(got[1])}
	sort.Strings(gotNames)
	want := []string{"another.git", "real.git"}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Errorf("repo[%d] = %s; want %s", i, gotNames[i], want[i])
		}
	}
}

func TestSweep_RunsAuditAndEventsPrune(t *testing.T) {
	root := t.TempDir()
	// Create a repo dir with an audit.log containing one old accept record.
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock() // Now() == 2026-01-01T12:00:00Z
	old := clock.Now().Add(-90 * 24 * time.Hour)

	rec, _ := json.Marshal(map[string]any{"time": old, "repo": "repo", "accept": true, "observed": false})
	if err := os.WriteFile(filepath.Join(repo, "audit.log"), append(rec, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	evrec, _ := json.Marshal(map[string]any{"ts": old, "event": "x", "ok": true})
	if err := os.WriteFile(filepath.Join(root, "_events.jsonl"), append(evrec, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig() // AuditAcceptRetention=30d, AuditRejectRetention=0, EventsRetention=30d
	sink := &recordingEvents{}
	r := NewRunnerWithTasks(cfg, root, root, "", &stubGC{}, nil, sink, clock)
	r.sweep(context.Background())

	st := r.Status()
	if len(st.LastAudit) != 1 || st.LastAudit[0].PrunedAccept != 1 {
		t.Fatalf("audit prune not wired: %+v", st.LastAudit)
	}
	if st.LastEvents.Pruned != 1 {
		t.Fatalf("events prune not wired: %+v", st.LastEvents)
	}
	if !slices.Contains(sink.names(), "maintenance-audit-pruned") {
		t.Fatalf("expected maintenance-audit-pruned event; got %v", sink.names())
	}
	if !slices.Contains(sink.names(), "maintenance-events-pruned") {
		t.Fatalf("expected maintenance-events-pruned event; got %v", sink.names())
	}
}

func TestSweep_GuardSkipsWhenRetentionZero(t *testing.T) {
	root := t.TempDir()
	// Create a repo dir with an old audit.log record that WOULD be pruned if
	// AuditAcceptRetention > 0.
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock() // Now() == 2026-01-01T12:00:00Z
	old := clock.Now().Add(-90 * 24 * time.Hour)

	rec, _ := json.Marshal(map[string]any{"time": old, "repo": "repo", "accept": true, "observed": false})
	if err := os.WriteFile(filepath.Join(repo, "audit.log"), append(rec, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// Zero both retention values so Tasks 5 and 6 are skipped entirely.
	cfg := DefaultConfig()
	cfg.AuditAcceptRetention = 0
	cfg.EventsRetention = 0

	sink := &recordingEvents{}
	r := NewRunnerWithTasks(cfg, root, root, "", &stubGC{}, nil, sink, clock)
	r.sweep(context.Background())

	st := r.Status()
	if len(st.LastAudit) != 0 {
		t.Errorf("LastAudit should be empty when AuditAcceptRetention=0; got %+v", st.LastAudit)
	}
	if st.LastEvents.Pruned != 0 {
		t.Errorf("LastEvents.Pruned should be 0 when EventsRetention=0; got %d", st.LastEvents.Pruned)
	}
	if slices.Contains(sink.names(), "maintenance-audit-pruned") {
		t.Errorf("maintenance-audit-pruned must not be emitted when retention=0; got %v", sink.names())
	}
}

func TestRunner_statusIsDefensiveCopy(t *testing.T) {
	root := makeFakeRepoTree(t, "x.git")
	gc := &stubGC{}
	events := &recordingEvents{}
	clock := newFakeClock()
	r := NewRunner(Config{Enabled: true, Interval: time.Hour}, root, gc, events, clock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	clock.Advance(time.Hour)
	for i := 0; i < 50; i++ {
		gc.mu.Lock()
		n := len(gc.calls)
		gc.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st1 := r.Status()
	if len(st1.PerRepo) != 1 {
		t.Fatalf("PerRepo = %d; want 1", len(st1.PerRepo))
	}

	// Mutating the snapshot must not affect future reads.
	st1.PerRepo[0].Repo = "mutated"
	st2 := r.Status()
	if st2.PerRepo[0].Repo == "mutated" {
		t.Error("Status() returns a shared slice; should be a defensive copy")
	}

	cancel()
	<-done
}
