// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package webhook delivers notification payloads to operator-configured URLs.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"nimblegate/internal/gateway/upstream"
)

// Auth carries the verification mode + credential for a webhook delivery.
// Mode "none" sends no auth header; "hmac" signs the payload and sends the
// signature in an X-Nimblegate-Signature header (or a configured override);
// "bearer" sends the secret as Authorization: Bearer <secret>.
type Auth struct {
	Mode       string // "hmac" | "bearer" | "none"
	Secret     string
	HeaderName string // optional override
}

// Client delivers webhooks. Wraps http.Client with sensible defaults.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a Client with a 10s timeout - sufficient for typical
// webhook receivers, short enough that a hung receiver doesn't tie up the
// daemon's drain loop.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Deliver POSTs payload to url with the configured auth mode. Errors are
// classified as upstream.ErrTransient (5xx, 429, network failures) or
// upstream.ErrPermanent (other 4xx, unknown auth mode) so the daemon can
// route to retry vs deadletter.
func (c *Client) Deliver(ctx context.Context, url string, payload []byte, auth Auth) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nimblegate/v0.1.0")

	switch auth.Mode {
	case "hmac":
		mac := SignHMAC(auth.Secret, payload)
		h := auth.HeaderName
		if h == "" {
			h = "X-Nimblegate-Signature"
		}
		req.Header.Set(h, "sha256="+mac)
	case "bearer":
		h := auth.HeaderName
		if h == "" {
			h = "Authorization"
		}
		req.Header.Set(h, "Bearer "+auth.Secret)
	case "none", "":
		// no auth
	default:
		return fmt.Errorf("%w: unknown auth mode %q", upstream.ErrPermanent, auth.Mode)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", upstream.ErrTransient, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return fmt.Errorf("%w: HTTP %d", upstream.ErrTransient, resp.StatusCode)
	}
	return fmt.Errorf("%w: HTTP %d", upstream.ErrPermanent, resp.StatusCode)
}

// SignHMAC returns hex-encoded HMAC-SHA256 of payload using secret.
// Public so receivers can verify signatures the same way the gateway signs.
func SignHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC checks an incoming signature against expected. Constant-time
// comparison to defeat timing attacks.
func VerifyHMAC(secret string, payload []byte, signature string) bool {
	expected := SignHMAC(secret, payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}
