// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"nimblegate/internal/gateway/upstream"
)

// RFC 4231 Test Case 2 vector for HMAC-SHA-256.
func TestSignHMAC_RFC4231Case2(t *testing.T) {
	key := "Jefe"
	data := []byte("what do ya want for nothing?")
	want := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	got := SignHMAC(key, data)
	if got != want {
		t.Errorf("SignHMAC mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestVerifyHMAC_RoundTrip(t *testing.T) {
	payload := []byte(`{"event":"push.rejected"}`)
	sig := SignHMAC("secret", payload)
	if !VerifyHMAC("secret", payload, sig) {
		t.Errorf("verify should accept matching signature")
	}
	if VerifyHMAC("wrong-secret", payload, sig) {
		t.Errorf("verify should reject mismatched secret")
	}
}

func TestDeliver_HMACSignatureHeader(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nimblegate-Signature")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	payload := []byte(`{"event":"test"}`)
	c := NewClient()
	err := c.Deliver(context.Background(), srv.URL, payload, Auth{Mode: "hmac", Secret: "secret"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	want := "sha256=" + SignHMAC("secret", payload)
	if gotSig != want {
		t.Errorf("signature header wrong:\n  got:  %s\n  want: %s", gotSig, want)
	}
}

func TestDeliver_BearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "bearer", Secret: "token123"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotAuth != "Bearer token123" {
		t.Errorf("bearer header wrong: %q", gotAuth)
	}
}

func TestDeliver_NoneSendsNoAuth(t *testing.T) {
	var sawAuth, sawSig bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") != ""
		sawSig = r.Header.Get("X-Nimblegate-Signature") != ""
		w.WriteHeader(200)
	}))
	defer srv.Close()

	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "none"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sawAuth || sawSig {
		t.Errorf("none mode should send no auth headers (auth=%v sig=%v)", sawAuth, sawSig)
	}
}

func TestDeliver_5xxIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "none"})
	if !errors.Is(err, upstream.ErrTransient) {
		t.Errorf("503 should be ErrTransient, got %v", err)
	}
}

func TestDeliver_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()
	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "none"})
	if !errors.Is(err, upstream.ErrTransient) {
		t.Errorf("429 should be ErrTransient, got %v", err)
	}
}

func TestDeliver_403IsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()
	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "none"})
	if !errors.Is(err, upstream.ErrPermanent) {
		t.Errorf("403 should be ErrPermanent, got %v", err)
	}
}

func TestDeliver_UnknownAuthModeIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	err := NewClient().Deliver(context.Background(), srv.URL, []byte(`{}`), Auth{Mode: "bogus"})
	if !errors.Is(err, upstream.ErrPermanent) {
		t.Errorf("unknown auth mode should be ErrPermanent, got %v", err)
	}
}
