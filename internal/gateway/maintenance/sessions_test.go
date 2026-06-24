// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"errors"
	"testing"
	"time"
)

type stubSweeper struct {
	called int
	err    error
}

func (s *stubSweeper) SweepExpiredSessions() error {
	s.called++
	return s.err
}

func TestRunSessionSweep_callsSweeper(t *testing.T) {
	sw := &stubSweeper{}
	now := func() time.Time { return time.Now() }
	res := runSessionSweep(now, sw)
	if sw.called != 1 {
		t.Errorf("called %d times; want 1", sw.called)
	}
	if res.Err != nil {
		t.Errorf("Err = %v; want nil", res.Err)
	}
	if res.Took.IsZero() {
		t.Error("Took should be set")
	}
}

func TestRunSessionSweep_nilSweeperIsNoop(t *testing.T) {
	now := func() time.Time { return time.Now() }
	res := runSessionSweep(now, nil)
	if res.Err != nil || !res.Took.IsZero() {
		t.Errorf("nil sweeper should produce zero result; got %+v", res)
	}
}

func TestRunSessionSweep_propagatesError(t *testing.T) {
	sw := &stubSweeper{err: errors.New("simulated db lock")}
	now := func() time.Time { return time.Now() }
	res := runSessionSweep(now, sw)
	if res.Err == nil {
		t.Error("expected error to propagate")
	}
}
