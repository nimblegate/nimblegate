// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewDashboardServer_setsEveryPhaseTimeout(t *testing.T) {
	s := newDashboardServer("127.0.0.1:0", http.NewServeMux())
	for _, c := range []struct {
		name string
		got  time.Duration
	}{
		{"ReadHeaderTimeout", s.ReadHeaderTimeout},
		{"ReadTimeout", s.ReadTimeout},
		{"WriteTimeout", s.WriteTimeout},
		{"IdleTimeout", s.IdleTimeout},
	} {
		if c.got <= 0 {
			t.Errorf("%s is unset; a connection can then hold a goroutine open forever", c.name)
		}
	}
	if s.MaxHeaderBytes <= 0 || s.MaxHeaderBytes > 1<<20 {
		t.Errorf("MaxHeaderBytes = %d; want a positive value below the 1 MiB default", s.MaxHeaderBytes)
	}
	// Headers must time out before the request as a whole, or the header
	// deadline never bites.
	if s.ReadHeaderTimeout > s.ReadTimeout {
		t.Errorf("ReadHeaderTimeout (%s) must not exceed ReadTimeout (%s)", s.ReadHeaderTimeout, s.ReadTimeout)
	}
	// The export streams a whole audit log; a write deadline under the read
	// deadline would cut long downloads off first.
	if s.WriteTimeout < s.ReadTimeout {
		t.Errorf("WriteTimeout (%s) should not be tighter than ReadTimeout (%s)", s.WriteTimeout, s.ReadTimeout)
	}
}

// TestDashboardsUseTimeoutServer is the regression gate, in the same shape as
// TestNoUnboundedReadInChecks: a future entry point that reaches for the
// stdlib one-liner brings the zero-value timeouts back with it.
func TestDashboardsUseTimeoutServer(t *testing.T) {
	raw := regexp.MustCompile(`\bhttp\.ListenAndServe(TLS)?\(`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if raw.Match(b) {
			t.Errorf("%s calls http.ListenAndServe directly; use newDashboardServer so the connection timeouts come with it", name)
		}
	}
}

func TestDashboardServer_closesAClientThatNeverFinishesItsHeaders(t *testing.T) {
	// The behaviour the timeouts exist for. Uses the real constructor and then
	// shortens the header deadline, so the test asserts the mechanism without
	// waiting the production 10s.
	srv := newDashboardServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.ReadHeaderTimeout = 150 * time.Millisecond
	ts := httptest.NewUnstartedServer(srv.Handler)
	ts.Config = srv
	ts.Start()
	defer ts.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// A request that never sends its blank terminating line.
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	if _, err := conn.Read(buf); err == nil {
		return // server answered (408) and is closing: also a pass
	} else if strings.Contains(err.Error(), "timeout") {
		t.Error("server held the half-open request instead of timing it out")
	}
}
