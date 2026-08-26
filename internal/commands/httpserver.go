// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"time"
)

// Timeout policy for both dashboards. The zero-value http.Server sets none of
// these, so a connection can hold a handler goroutine open indefinitely: a
// client that opens sockets and dribbles headers a byte at a time costs itself
// nothing and costs the daemon a goroutine each. Request bodies are the one
// phase the stdlib already covers - ParseForm caps urlencoded bodies at 10 MiB.
const (
	// dashboardReadHeaderTimeout is the cut-off that matters: headers must
	// arrive within it, whatever the body does afterwards.
	dashboardReadHeaderTimeout = 10 * time.Second
	dashboardReadTimeout       = 30 * time.Second
	// dashboardWriteTimeout is generous because /feed/export streams the whole
	// (retention-pruned) audit log - the one response that can legitimately be
	// large on a slow link.
	dashboardWriteTimeout = 120 * time.Second
	dashboardIdleTimeout  = 120 * time.Second
	// dashboardMaxHeaderBytes sits well above a session cookie and well below
	// the 1 MiB stdlib default.
	dashboardMaxHeaderBytes = 64 << 10
)

// newDashboardServer builds the HTTP server both dashboards listen on, with a
// timeout on every phase of a connection.
func newDashboardServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: dashboardReadHeaderTimeout,
		ReadTimeout:       dashboardReadTimeout,
		WriteTimeout:      dashboardWriteTimeout,
		IdleTimeout:       dashboardIdleTimeout,
		MaxHeaderBytes:    dashboardMaxHeaderBytes,
	}
}
