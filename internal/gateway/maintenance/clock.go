// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import "time"

// RealClock wraps the stdlib time package for production use. Tests inject
// their own Clock instead.
type RealClock struct{}

func (RealClock) Now() time.Time                   { return time.Now() }
func (RealClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }
