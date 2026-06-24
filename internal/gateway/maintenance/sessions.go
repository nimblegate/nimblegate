// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import "time"

// SessionSweeper deletes expired session rows. Real impl is auth.Store
// (its existing SweepExpiredSessions method). Tests use a stub.
type SessionSweeper interface {
	SweepExpiredSessions() error
}

// SessionSweepResult is per-sweep state for /health. Took is best-effort
// wall-clock timing (the underlying SQLite delete is single-statement,
// so this is informational, not for alerting).
type SessionSweepResult struct {
	Took time.Time // when this sweep ran (StartedAt analogue)
	Err  error
}

// runSessionSweep wraps a SessionSweeper call with timing + error capture.
// Returns a zero result if sweeper is nil (the daemon may not have an auth
// store wired in some test/dev configs).
func runSessionSweep(now func() time.Time, sweeper SessionSweeper) SessionSweepResult {
	if sweeper == nil {
		return SessionSweepResult{}
	}
	res := SessionSweepResult{Took: now()}
	res.Err = sweeper.SweepExpiredSessions()
	return res
}
